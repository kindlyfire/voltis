import createDOMPurify, { type Config, type DOMPurify } from 'dompurify'
import { API_URL } from '@/utils/fetch'
import { normalizedTextLength } from './documentRange'
import {
    docBody,
    docRoot,
    elementsUnder,
    getAttr,
    hasAttr,
    removeAttr,
    setAttr,
    tagOf,
} from './domSafe'
import { isExternalRef, resolveEpubRef } from './epubPaths'

export interface PrepareContext {
    contentId: string
    mtime: string | null
    spineHrefs: Set<string>
    readerPath: string
    fetchText: (path: string, signal?: AbortSignal) => Promise<string>
    signal?: AbortSignal
}

export interface PreparedDocument {
    href: string
    doc: Document
    styles: string[]
    rootAttributes: Array<[string, string]>
    textLength: number
}

const MAX_IMPORT_DEPTH = 4
const DOCUMENT_EXT_RE = /\.(?:x?html?|xht)$/i
const RESOURCE_ATTRS = ['src', 'poster', 'data', 'background']
const SRCSET_ATTRS = ['srcset', 'imagesrcset']
const FORBIDDEN_ATTRS = ['action', 'formaction', 'ping']
const SVG_REF_TAGS = new Set(['image', 'use', 'feimage', 'textpath', 'mpath'])
const PLACEHOLDER = '\u0001'
const PLACEHOLDER_RE = /\u0001(\d+)\u0001/g
const CSS_URL_RE = /url\(\s*(['"]?)([^'")]*)\1\s*\)/gi
const CSS_IMPORT_RE =
    /@import(?:\s|\/\*[\s\S]*?\*\/)*(?:url\(\s*(['"]?)([^'")]*)\1\s*\)|(['"])([^'"]*)\3)([^;]*);?/gi
/** A reference we refuse to rewrite must not survive: relative to the reader's
 * own origin it would become a credentialed request to our API. */
const REJECTED_CSS_URL = 'url("about:invalid")'

let purifier: DOMPurify | null = null

function sanitizer(): DOMPurify {
    purifier ??= createDOMPurify(window)
    return purifier
}

function resourceUrl(contentId: string, mtime: string | null, path: string): string {
    const params = new URLSearchParams({ path })
    if (mtime) params.set('v', mtime)
    return `${API_URL}/files/book-resource/${contentId}?${params}`
}

export function fetchBookResource(contentId: string, mtime: string | null) {
    return async (path: string, signal?: AbortSignal) => {
        const res = await fetch(resourceUrl(contentId, mtime, path), {
            credentials: 'include',
            signal,
        })
        if (!res.ok) throw new Error(`Failed to fetch ${path}`)
        return res.text()
    }
}

function readerUrl(ctx: PrepareContext, href: string, fragment: string): string {
    const params = new URLSearchParams({ ch: href })
    if (fragment) params.set('frag', fragment)
    return `${ctx.readerPath}?${params}`
}

type RefOutcome = { kind: 'keep' } | { kind: 'rewrite'; url: string } | { kind: 'reject' }

function classifyRef(ctx: PrepareContext, base: string, ref: string): RefOutcome {
    const trimmed = ref.trim()
    if (!trimmed) return { kind: 'reject' }
    // data: is inert, '#' stays inside the document (SVG filters, gradients).
    if (/^data:/i.test(trimmed) || trimmed.startsWith('#')) return { kind: 'keep' }
    const target = resolveEpubRef(base, trimmed)
    if (!target) return { kind: 'reject' }
    const url = resourceUrl(ctx.contentId, ctx.mtime, target.href)
    return {
        kind: 'rewrite',
        url: target.fragment ? `${url}#${encodeURIComponent(target.fragment)}` : url,
    }
}

function rewriteCssUrls(css: string, base: string, ctx: PrepareContext): string {
    return css.replace(CSS_URL_RE, (match, _quote, ref: string) => {
        const outcome = classifyRef(ctx, base, ref)
        if (outcome.kind === 'keep') return match
        return outcome.kind === 'rewrite' ? `url("${outcome.url}")` : REJECTED_CSS_URL
    })
}

function mediaWrap(css: string, media: string | null): string {
    const condition = media?.trim()
    return condition && condition.toLowerCase() !== 'all' ? `@media ${condition} {\n${css}\n}` : css
}

function consumeSupports(text: string): { condition: string; rest: string } | null {
    const open = /^supports\(/i.exec(text)
    if (!open) return null
    let depth = 0
    let quote = ''
    for (let i = open[0].length - 1; i < text.length; i++) {
        const char = text[i]!
        if (quote) {
            if (char === '\\') i++
            else if (char === quote) quote = ''
            continue
        }
        if (char === '"' || char === "'") quote = char
        else if (char === '(') depth++
        else if (char === ')' && --depth === 0) {
            return {
                condition: text.slice(open[0].length, i).trim(),
                rest: text.slice(i + 1).trim(),
            }
        }
    }
    return null
}

/** Rebuilds the `layer()`/`supports()`/media conditions an inlined `@import`
 * would otherwise lose. */
function wrapImportConditions(css: string, tail: string): string {
    let rest = tail.trim()
    let out = css
    let layer: string | null = null
    let supports: string | null = null

    const layerMatch = /^layer(?:\(\s*([^)]*?)\s*\))?/i.exec(rest)
    if (layerMatch) {
        layer = layerMatch[1]?.trim() ?? ''
        rest = rest.slice(layerMatch[0].length).trim()
    }
    const consumed = consumeSupports(rest)
    if (consumed) {
        supports = consumed.condition
        rest = consumed.rest
    }

    out = mediaWrap(out, rest)
    if (supports) {
        out = `@supports ${supports.startsWith('(') ? supports : `(${supports})`} {\n${out}\n}`
    }
    if (layer !== null) out = `@layer${layer ? ` ${layer}` : ''} {\n${out}\n}`
    return out
}

async function inlineImport(
    args: string[],
    base: string,
    ctx: PrepareContext,
    ancestors: readonly string[],
    depth: number
): Promise<string> {
    const ref = args[2] || args[4] || ''
    const target = ref ? resolveEpubRef(base, ref) : null
    // Dropped rather than left in place: the browser would fetch it relative
    // to the app origin.
    if (!target || depth >= MAX_IMPORT_DEPTH || ancestors.includes(target.href)) return ''
    const text = await ctx.fetchText(target.href, ctx.signal).catch(() => '')
    if (!text) return ''
    const inner = await rewriteCss(text, target.href, ctx, [...ancestors, target.href], depth + 1)
    return wrapImportConditions(inner, args[5] ?? '')
}

/** Own `url()` first, then inlined imports, so nothing is rewritten twice and
 * each sheet resolves against its own EPUB path. Accepted bypasses: escaped
 * identifiers, exotic quoting, `var()` indirection and `image-set()` can each
 * still carry an unrewritten reference through. */
async function rewriteCss(
    css: string,
    base: string,
    ctx: PrepareContext,
    ancestors: readonly string[] = [],
    depth = 0
): Promise<string> {
    const imports: Array<Promise<string>> = []
    const placeheld = css.replace(CSS_IMPORT_RE, (...args: unknown[]) => {
        const index = imports.length
        imports.push(inlineImport(args as string[], base, ctx, ancestors, depth))
        return `${PLACEHOLDER}${index}${PLACEHOLDER}`
    })

    const rewritten = rewriteCssUrls(placeheld, base, ctx)
    if (!imports.length) return rewritten
    const inlined = await Promise.all(imports)
    return rewritten.replace(PLACEHOLDER_RE, (_match, index: string) => inlined[Number(index)]!)
}

function rewriteSrcset(ctx: PrepareContext, base: string, value: string): string | null {
    const candidates: string[] = []
    for (const part of value.split(',')) {
        const trimmed = part.trim()
        if (!trimmed) continue
        const space = trimmed.search(/\s/)
        const ref = space === -1 ? trimmed : trimmed.slice(0, space)
        const rest = space === -1 ? '' : trimmed.slice(space)
        const outcome = classifyRef(ctx, base, ref)
        if (outcome.kind === 'reject') continue
        candidates.push((outcome.kind === 'rewrite' ? outcome.url : ref) + rest)
    }
    return candidates.length ? candidates.join(', ') : null
}

function rewriteAnchor(ctx: PrepareContext, base: string, el: Element) {
    const href = getAttr(el, 'href')
    if (!href) return
    if (isExternalRef(href)) {
        setAttr(el, 'target', '_blank')
        setAttr(el, 'rel', 'noopener noreferrer')
        return
    }
    const target = resolveEpubRef(base, href)
    // An off-spine document is outside the flow but may still be readable, so
    // it stays a link; the session decides what opening it means.
    const readable =
        target && (ctx.spineHrefs.has(target.href) || DOCUMENT_EXT_RE.test(target.href))
    if (!target || !readable) {
        removeAttr(el, 'href')
        setAttr(el, 'data-book-missing', '1')
        return
    }
    setAttr(el, 'data-book-href', target.href)
    setAttr(el, 'data-book-frag', target.fragment)
    setAttr(el, 'href', readerUrl(ctx, target.href, target.fragment))
}

function rewriteAttributes(el: Element, tag: string, base: string, ctx: PrepareContext) {
    for (const attr of RESOURCE_ATTRS) {
        const value = getAttr(el, attr)
        if (value == null) continue
        const outcome = classifyRef(ctx, base, value)
        if (outcome.kind === 'rewrite') setAttr(el, attr, outcome.url)
        else if (outcome.kind === 'reject') removeAttr(el, attr)
    }

    for (const attr of SRCSET_ATTRS) {
        const value = getAttr(el, attr)
        if (value == null) continue
        const rewritten = rewriteSrcset(ctx, base, value)
        if (rewritten) setAttr(el, attr, rewritten)
        else removeAttr(el, attr)
    }

    if (SVG_REF_TAGS.has(tag)) {
        for (const attr of ['href', 'xlink:href']) {
            const value = getAttr(el, attr)
            if (value == null) continue
            const outcome = classifyRef(ctx, base, value)
            if (outcome.kind === 'rewrite') setAttr(el, attr, outcome.url)
            else if (outcome.kind === 'reject') removeAttr(el, attr)
        }
    }
}

async function rewriteDocument(doc: Document, base: string, ctx: PrepareContext) {
    const styles: string[] = []
    const jobs: Promise<void>[] = []

    for (const el of elementsUnder(doc)) {
        const tag = tagOf(el)

        if (tag === 'style') {
            const index = styles.push('') - 1
            const media = getAttr(el, 'media')
            const css = el.textContent ?? ''
            jobs.push(
                rewriteCss(css, base, ctx).then(out => {
                    styles[index] = mediaWrap(out, media)
                })
            )
            el.remove()
            continue
        }

        if (tag === 'link') {
            const rel = (getAttr(el, 'rel') ?? '').toLowerCase()
            const href = getAttr(el, 'href')
            const target = href && !isExternalRef(href) ? resolveEpubRef(base, href) : null
            if (rel.split(/\s+/).includes('stylesheet') && target) {
                const index = styles.push('') - 1
                const media = getAttr(el, 'media')
                jobs.push(
                    ctx
                        .fetchText(target.href, ctx.signal)
                        .then(css => rewriteCss(css, target.href, ctx, [target.href]))
                        .then(
                            out => {
                                styles[index] = mediaWrap(out, media)
                            },
                            () => {}
                        )
                )
            }
            el.remove()
            continue
        }

        for (const attr of FORBIDDEN_ATTRS) removeAttr(el, attr)

        if (tag === 'a') rewriteAnchor(ctx, base, el)
        else rewriteAttributes(el, tag, base, ctx)

        const inline = getAttr(el, 'style')
        if (inline && /url\(/i.test(inline)) setAttr(el, 'style', rewriteCssUrls(inline, base, ctx))
    }

    await Promise.all(jobs)
    return styles.filter(Boolean)
}

const SANITIZE_CONFIG: Config = {
    ADD_TAGS: ['link'],
    ADD_ATTR: ['target', 'rel', 'href', 'name', 'srcset', 'sizes', 'media'],
    ALLOW_DATA_ATTR: false,
    WHOLE_DOCUMENT: true,
    // Fragment resolution, page boundaries and internal links all hinge on
    // authored ids surviving, and SANITIZE_DOM drops every `id`/`name` that
    // collides with a document or form property. Forms are dropped instead
    // (HTMLFormElement's named properties override inherited members), and
    // every traversal here goes through prototype-bound accessors, because
    // `name` on an img or object still shadows Document members.
    SANITIZE_DOM: false,
    FORBID_TAGS: ['form', 'input', 'select', 'textarea', 'button', 'fieldset', 'output'],
}

export async function prepareDocument(
    html: string,
    href: string,
    ctx: PrepareContext
): Promise<PreparedDocument> {
    const sanitized = sanitizer().sanitize(html, { ...SANITIZE_CONFIG })
    const doc = new DOMParser().parseFromString(sanitized, 'text/html')
    const styles = await rewriteDocument(doc, href, ctx)
    return {
        href,
        doc,
        styles,
        // After rewriting: the root's own attributes carry references too.
        rootAttributes: Array.from(docRoot(doc).attributes).map(
            attr => [attr.name, attr.value] as [string, string]
        ),
        textLength: normalizedTextLength(docBody(doc)),
    }
}

/** The page frame sits on the host, not on `body`: EPUB stylesheets are
 * appended after this one and routinely reset `body { margin: 0 }`. Nothing in
 * a book can target `:host`. */
const READER_CSS = `
:host {
    display: block;
    box-sizing: border-box;
    max-width: var(--reader-max-width, calc(45em + 4rem));
    margin: 0 auto;
    padding: 2rem;
    color-scheme: dark light;
    font-family: var(--reader-font-family, Georgia, 'Times New Roman', serif);
    line-height: var(--reader-line-height, 1.8);
    font-size: var(--reader-font-size, 1.1rem);
}
html, body { display: block; }
body { margin: 0; }
img, svg, video { max-width: 100%; height: auto; }
a { color: inherit; }
a[data-book-missing] { text-decoration: line-through; opacity: 0.7; }
`

export const MONO_STACK = "ui-monospace, 'SFMono-Regular', Menlo, Consolas, monospace"

/** Repeated `:not(#x)` outranks the class selectors books style their text
 * with: between two `!important` declarations, specificity still decides.
 * `:is()` costs nothing extra, it takes the specificity of its argument. */
const BEAT = ':not(#reader-user-css):not(#reader-user-css)'
/** `html` and `body` included: a book that rescales either one would otherwise
 * resize every paragraph inheriting from it. */
const TEXT = ':is(html, body, p, li, dd, dt, blockquote, div, span, td, th, figcaption)'
const MONO = ':is(pre, code, kbd, samp, tt)'

const USER_STYLE_MARK = 'data-reader-user'

/** Sits after the book's own sheets so the reader's font settings win. The
 * sizes are `inherit` rather than values: that keeps every element tied to the
 * one size on `:host` without flattening headings. */
function userCss(publisherFonts: boolean): string {
    let css = `
${TEXT}${BEAT} {
    font-size: inherit !important;
    line-height: inherit !important;
}
`
    if (!publisherFonts) {
        css += `
${BEAT} {
    font-family: inherit !important;
}
${MONO}${BEAT}, ${MONO} ${BEAT} {
    font-family: ${MONO_STACK} !important;
}
`
    }
    return css
}

/** Swaps the user sheet of an already mounted tree, for settings that the
 * inherited custom properties can't carry. */
export function updateUserStyles(host: HTMLElement, publisherFonts = false) {
    const style = host.shadowRoot?.querySelector(`style[${USER_STYLE_MARK}]`)
    if (style) style.textContent = userCss(publisherFonts)
}

/** Attaches a document's (possibly pruned) body into its own shadow root,
 * keeping the `html`/`body` elements so authored tag selectors still match. */
export function mountTree(
    host: HTMLElement,
    prepared: PreparedDocument,
    body: Element,
    publisherFonts = false
): Element {
    const shadow = host.shadowRoot ?? host.attachShadow({ mode: 'open' })
    const base = document.createElement('style')
    base.textContent = READER_CSS

    const root = document.createElement('html')
    for (const [name, value] of prepared.rootAttributes) {
        if (name !== 'manifest') root.setAttribute(name, value)
    }
    const mounted = document.createElement('body')
    for (const attr of Array.from(body.attributes)) {
        if (!hasAttr(mounted, attr.name)) mounted.setAttribute(attr.name, attr.value)
    }
    mounted.append(...Array.from(body.childNodes))
    root.append(mounted)

    const nodes: Node[] = [base]
    for (const css of prepared.styles) {
        const style = document.createElement('style')
        style.textContent = css
        nodes.push(style)
    }

    const user = document.createElement('style')
    user.setAttribute(USER_STYLE_MARK, '')
    user.textContent = userCss(publisherFonts)
    nodes.push(user, root)
    shadow.replaceChildren(...nodes)
    return mounted
}
