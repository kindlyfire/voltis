import { useDebounceFn } from '@vueuse/core'
import { computed, markRaw, reactive, toRefs } from 'vue'
import { contentApi } from '@/utils/api/content'
import type {
    BookLocator,
    BookStructure,
    Content,
    ReadingStatus,
    UserToContent,
} from '@/utils/api/types'
import { getLayoutTop, queryClient } from '@/utils/misc'
import { chooseEntryPage, entryKey, isBookLocator, type BookEntry } from './bookEntry'
import { flowWeights, progressPercent, type FlowWeights } from './bookProgress'
import {
    boundaryDocumentHrefs,
    buildPages,
    mapEntriesToPages,
    pageIndexForPosition,
    pagesContainingOffset,
    resolveFlowPosition,
    resolveTargetElement,
    spineIndexOf,
    type BookPage,
    type FlowPosition,
    type PageSlice,
} from './buildPages'
import {
    elementAtTextOffset,
    pathOfNode,
    pruneToRange,
    textOffsetAtPath,
    textOffsetOfNode,
} from './documentRange'
import {
    childNodesOf,
    childrenOf,
    docBody,
    findTarget,
    getAttr,
    hasAttr,
    parentOf,
    previousOf,
    tagOf,
} from './domSafe'
import {
    fetchBookResource,
    mountTree,
    prepareDocument,
    type PrepareContext,
    type PreparedDocument,
} from './prepareDocument'

const SETTLE_TIMEOUT = 2000
const SCROLL_MARGIN = 8
const CAPTURE_DEBOUNCE = 250
const PERSIST_DEBOUNCE = 1000
const RESIZE_DEBOUNCE = 200
const HISTORY_STAMP_INTERVAL = 500
const ANCHOR_SEARCH_LIMIT = 500

const BLOCK_TAGS = new Set([
    'p',
    'div',
    'section',
    'article',
    'blockquote',
    'pre',
    'figure',
    'img',
    'table',
    'tr',
    'td',
    'li',
    'h1',
    'h2',
    'h3',
    'h4',
    'h5',
    'h6',
])

export interface BookAnchor {
    href: string
    fragment: string
}

export interface BookNav {
    push(target: BookAnchor): void
    replace(target: BookAnchor): void
    /** Stores the reading position on the current history entry, so Back and
     * Forward return to the passage rather than the page top. */
    saveLocator(locator: BookLocator): void
    historyLocator(): BookLocator | null
}

export interface BookSessionValues {
    contentId: string
    content: Content | null
    structure: BookStructure | null
    pages: BookPage[]
    entryPages: Record<string, number>
    pageIndex: number
    standalone: { href: string; title: string } | null
    fallback: boolean
    loading: boolean
    error: string | null
    notice: string | null
    restoring: boolean
    percent: number
}

type Destination =
    | { kind: 'page'; index: number }
    | { kind: 'standalone'; href: string; title: string }

interface MountedSlice {
    slice: PageSlice
    prepared: PreparedDocument
    holder: HTMLElement
    root: Element
    startOffset: number
}

function firstVisibleBlock(root: Element, top: number): Element | null {
    let best: Element | null = null
    const visit = (el: Element) => {
        for (const child of childrenOf(el)) {
            const rect = child.getBoundingClientRect()
            if (rect.width === 0 && rect.height === 0) continue
            if (rect.bottom <= top) continue
            if (BLOCK_TAGS.has(tagOf(child))) best = child
            visit(child)
            return
        }
    }
    visit(root)
    return best
}

/** The nearest authored id at or before `el` in document order, used to
 * recover a position when the document changed under a stored offset. */
function precedingAnchorId(el: Element, root: Element): string | undefined {
    let node: Node | null = el
    for (let steps = 0; node && node !== root && steps < ANCHOR_SEARCH_LIMIT; steps++) {
        if (node.nodeType === Node.ELEMENT_NODE) {
            const id = getAttr(node as Element, 'id') ?? getAttr(node as Element, 'name')
            if (id) return id
        }
        const previous = previousOf(node)
        if (!previous) {
            node = parentOf(node)
            continue
        }
        node = previous
        for (let last = childNodesOf(node).at(-1); last; last = childNodesOf(node).at(-1)) {
            node = last
        }
    }
    return undefined
}

function delay(ms: number) {
    return new Promise(resolve => setTimeout(resolve, ms))
}

export function createBookSession(contentId: string, entry: BookEntry, nav: BookNav) {
    const state = reactive<BookSessionValues>({
        contentId,
        content: null,
        structure: null,
        pages: [],
        entryPages: {},
        pageIndex: 0,
        standalone: null,
        fallback: false,
        loading: true,
        error: null,
        notice: null,
        restoring: false,
        percent: 0,
    })

    let disposed = false
    let navToken = 0
    let restoreCancelled = false
    let programmaticScrolls = 0
    let host: HTMLElement | null = null
    let sentinel: HTMLElement | null = null
    let mounted: MountedSlice[] = []
    let ctx: PrepareContext | null = null
    let userData: UserToContent | null = null
    let weights: FlowWeights = { weights: [], total: 0 }
    let pendingLocator: BookLocator | null = null
    let writeChain = Promise.resolve()
    let closing = false
    let completed = false
    let inputSincePage = false
    let captureEnabled = false
    let currentEntryKey = entryKey(entry)
    let resizeLocator: BookLocator | null = null
    let pendingEntry: BookEntry | null = null
    let lastStamp = 0
    let stampTimer: ReturnType<typeof setTimeout> | null = null
    let writeController: AbortController | null = null
    let standaloneReturn: { pageIndex: number; locator: BookLocator | null } | null = null

    const hostWaiters: Array<() => void> = []
    const controller = new AbortController()
    const trees = new Map<string, Promise<PreparedDocument>>()
    const prepared = new Map<string, PreparedDocument>()
    const docs = new Map<string, Document>()

    function structure(): BookStructure {
        return state.structure!
    }

    /** Unlike `begin`, leaves capture armed: the reader is still on a mounted
     * page until a replacement arrives. */
    function cancelPending(): number {
        restoreCancelled = false
        clearStampTimer()
        return ++navToken
    }

    function begin(): number {
        captureEnabled = false
        return cancelPending()
    }

    function isCurrent(token: number) {
        return !disposed && token === navToken
    }

    function prepare(href: string): Promise<PreparedDocument> {
        const key = `${href}@${state.content?.file_mtime ?? ''}`
        let job = trees.get(key)
        if (!job) {
            job = contentApi
                .bookDocument(contentId, href, { signal: controller.signal })
                .then(html => prepareDocument(html, href, ctx!))
                .then(result => {
                    prepared.set(href, result)
                    docs.set(href, result.doc)
                    return result
                })
            trees.set(key, job)
        }
        return job
    }

    async function preparedFor(href: string): Promise<PreparedDocument | null> {
        return prepared.get(href) ?? (await prepare(href).catch(() => null))
    }

    function fallbackPages(source: BookStructure): BookPage[] {
        const linear = source.spine
            .map((item, index) => ({ item, index }))
            .filter(x => x.item.linear)
        return linear.map(({ item, index }, i) => ({
            index: i,
            title: item.title || item.href,
            target: { href: item.href, fragment: '' },
            start: { spineIndex: index, path: [] },
            end: linear[i + 1] ? { spineIndex: linear[i + 1]!.index, path: [] } : null,
            slices: [{ href: item.href, spineIndex: index, start: null, end: null }],
        }))
    }

    async function init() {
        try {
            const [content, source] = await Promise.all([
                contentApi.get(contentId, { signal: controller.signal }),
                contentApi.bookStructure(contentId, { signal: controller.signal }),
            ])
            if (disposed) return

            state.content = content
            userData = content.user_data ?? null
            ctx = {
                contentId,
                mtime: content.file_mtime,
                spineHrefs: new Set(source.spine.map(item => item.href)),
                readerPath: `/r/${contentId}`,
                fetchText: fetchBookResource(contentId, content.file_mtime),
                signal: controller.signal,
            }
            state.structure = markRaw(source)
            weights = flowWeights(source.spine)

            await Promise.all(
                boundaryDocumentHrefs(source).map(href => prepare(href).catch(() => null))
            )
            if (disposed) return

            const pages = buildPages(source, docs)
            state.fallback = pages.length === 0
            state.pages = markRaw(pages.length ? pages : fallbackPages(source))
            if (!state.pages.length) {
                state.error = 'This book has no readable content.'
                state.loading = false
                return
            }
            state.entryPages = Object.fromEntries(mapEntriesToPages(source, state.pages, docs))
            state.loading = false
            await applyEntry(pendingEntry ?? entry, begin(), true)
        } catch (err) {
            if (disposed) return
            console.error(err)
            state.error = err instanceof Error ? err.message : String(err)
            state.loading = false
        }
    }

    async function positionFor(href: string, fragment: string): Promise<FlowPosition | null> {
        const source = structure()
        const index = spineIndexOf(source, href)
        if (index === -1 || !source.spine[index]!.linear) return null
        if (fragment && !prepared.has(href)) await preparedFor(href)
        return (
            resolveFlowPosition(source, docs, href, fragment) ??
            resolveFlowPosition(source, docs, href, '')
        )
    }

    function positionLocator(position: FlowPosition): BookLocator {
        const href = structure().spine[position.spineIndex]!.href
        const doc = docs.get(href)
        const body = doc ? docBody(doc) : null
        return {
            version: 1,
            href,
            textOffset: body ? textOffsetAtPath(body, position.path) : 0,
        }
    }

    function savedLocator(): BookLocator | null {
        const value = userData?.progress?.book
        return isBookLocator(value) && spineIndexOf(structure(), value.href) !== -1 ? value : null
    }

    async function pageForLocator(locator: BookLocator): Promise<number | null> {
        const source = structure()
        const spineIndex = spineIndexOf(source, locator.href)
        if (spineIndex === -1 || !source.spine[spineIndex]!.linear) return null
        const doc = await preparedFor(locator.href)
        if (!doc) return null

        const anchorPage = () => {
            const body = docBody(doc.doc)!
            const el = locator.anchorId ? findTarget(body, locator.anchorId) : null
            const path = el ? pathOfNode(body, el) : null
            return path ? pageIndexForPosition(state.pages, { spineIndex, path }) : -1
        }

        if (locator.textOffset > doc.textLength) {
            const page = anchorPage()
            return page === -1 ? null : page
        }

        // Textless or equal-offset intervals can't be told apart by offset
        // alone, so the anchor decides between them.
        const candidates = pagesContainingOffset(
            state.pages,
            locator.href,
            locator.textOffset,
            doc.doc
        )
        if (!candidates.length) return null
        if (candidates.length > 1) {
            const page = anchorPage()
            if (candidates.includes(page)) return page
        }
        return candidates[0]!
    }

    async function applyEntry(current: BookEntry, token: number, initial: boolean) {
        if (!isCurrent(token)) return
        if (!state.pages.length) {
            settleTransition(token)
            return
        }
        const source = structure()

        if (current.ch) {
            const index = spineIndexOf(source, current.ch)
            if (index === -1 || !source.spine[index]!.linear) {
                const shown = await mountStandalone(current.ch, current.frag ?? '', token)
                if (shown) return
                if (!initial) {
                    settleTransition(token)
                    return
                }
            }
        }

        const urlPosition = current.ch ? await positionFor(current.ch, current.frag ?? '') : null
        if (!isCurrent(token)) return
        const urlPage = urlPosition ? pageIndexForPosition(state.pages, urlPosition) : null

        const locator = nav.historyLocator() ?? (initial ? savedLocator() : null)
        const locatorPage = locator ? await pageForLocator(locator) : null
        if (!isCurrent(token)) return

        const choice = chooseEntryPage(urlPage, locatorPage)
        const anchor =
            !choice.useLocator && current.ch && current.frag
                ? { href: current.ch, fragment: current.frag }
                : null
        await navigateTo(
            choice.pageIndex,
            {
                locator: choice.useLocator
                    ? locator
                    : urlPosition
                      ? positionLocator(urlPosition)
                      : null,
                anchor,
            },
            token
        )
        if (!isCurrent(token)) return
        canonicalize(anchor)
    }

    function canonicalize(anchor: BookAnchor | null) {
        const page = state.pages[state.pageIndex]
        if (!page || state.standalone) return
        const target = anchor ?? page.target
        currentEntryKey = entryKey({ ch: target.href, frag: target.fragment || null })
        nav.replace(target)
    }

    function scrollWindowTo(top: number) {
        if (disposed) return
        programmaticScrolls++
        window.scrollTo({ top: Math.max(0, top), behavior: 'instant' })
        requestAnimationFrame(() => {
            programmaticScrolls = Math.max(0, programmaticScrolls - 1)
        })
    }

    function scrollToElement(el: Element) {
        scrollWindowTo(
            window.scrollY + el.getBoundingClientRect().top - getLayoutTop() - SCROLL_MARGIN
        )
    }

    /** Guarded at the point of entry as well as on disposal: the queue can
     * still be appended to after `dispose` has swept it. */
    function waitForHost(): Promise<void> {
        if (host || disposed) return Promise.resolve()
        return new Promise(resolve => {
            hostWaiters.push(resolve)
        })
    }

    async function buildSlices(slices: PageSlice[], token: number): Promise<MountedSlice[] | null> {
        const documents = await Promise.all(slices.map(slice => prepare(slice.href)))
        if (!isCurrent(token)) return null
        await waitForHost()
        if (!isCurrent(token) || !host) return null

        return slices.map((slice, i) => {
            const source = documents[i]!
            const body = docBody(source.doc)!.cloneNode(true) as HTMLElement
            pruneToRange(body, slice.start, slice.end)
            const holder = document.createElement('div')
            holder.addEventListener('click', onClick)
            return {
                slice,
                prepared: source,
                holder,
                root: mountTree(holder, source, body),
                startOffset: textOffsetAtPath(docBody(source.doc)!, slice.start),
            }
        })
    }

    /** Where the destination becomes observable. Nothing above writes reader
     * state, so a cancelled navigation leaves none of it half-moved. */
    function commit(next: MountedSlice[], destination: Destination) {
        host!.replaceChildren(...next.map(slice => slice.holder))
        mounted = next
        inputSincePage = false
        lastStamp = 0
        state.error = null
        if (destination.kind === 'page') {
            state.pageIndex = destination.index
            state.standalone = null
            standaloneReturn = null
        } else {
            state.standalone = { href: destination.href, title: destination.title }
        }
    }

    async function settle() {
        const jobs: Promise<unknown>[] = []
        const fonts = (document as Document & { fonts?: FontFaceSet }).fonts
        if (fonts?.ready) jobs.push(fonts.ready)
        for (const slice of mounted) {
            for (const img of Array.from(slice.root.querySelectorAll('img'))) {
                if (img.complete) continue
                jobs.push(
                    new Promise<void>(resolve => {
                        img.addEventListener('load', () => resolve(), { once: true })
                        img.addEventListener('error', () => resolve(), { once: true })
                    })
                )
            }
        }
        if (!jobs.length) return
        await Promise.race([Promise.all(jobs), delay(SETTLE_TIMEOUT)])
    }

    function locateInMounted(locator: BookLocator): Element | null {
        for (const slice of mounted) {
            if (slice.slice.href !== locator.href) continue
            const relative = locator.textOffset - slice.startOffset
            if (relative < 0) continue
            const el = elementAtTextOffset(slice.root, relative)
            if (el) return el
        }
        if (!locator.anchorId) return null
        for (const slice of mounted) {
            if (slice.slice.href !== locator.href) continue
            const el = findTarget(slice.root, locator.anchorId)
            if (el) return el
        }
        return null
    }

    function locateAnchor(anchor: BookAnchor): Element | null {
        for (const slice of mounted) {
            if (slice.slice.href !== anchor.href) continue
            const el = findTarget(slice.root, anchor.fragment)
            if (el) return el
        }
        return null
    }

    /** The single end of a navigation: whatever is mounted now is the page the
     * reader has, whether this navigation put it there or not. */
    function settleTransition(token: number) {
        if (!isCurrent(token)) return
        state.restoring = false
        restoreCancelled = false
        captureEnabled = mounted.length > 0 && !state.standalone
        captureNow()
    }

    async function navigateTo(
        index: number,
        target: { locator?: BookLocator | null; anchor?: BookAnchor | null },
        token: number
    ) {
        const page = state.pages[index]
        if (!isCurrent(token)) return
        if (!page) {
            settleTransition(token)
            return
        }
        state.restoring = true
        captureEnabled = false
        const hadMounted = mounted.length > 0

        let next: MountedSlice[] | null = null
        try {
            next = await buildSlices(page.slices, token)
        } catch (err) {
            if (isCurrent(token)) {
                console.error(err)
                // A reader already on a page keeps it; only a failed first
                // load leaves nothing to show.
                if (hadMounted) state.notice = 'That part of the book could not be loaded.'
                else state.error = err instanceof Error ? err.message : String(err)
            }
        }
        if (!isCurrent(token)) return
        if (!next) {
            settleTransition(token)
            return
        }

        commit(next, { kind: 'page', index })
        scrollWindowTo(0)
        await settle()
        if (!isCurrent(token)) return

        if (!restoreCancelled) {
            const el =
                (target.anchor ? locateAnchor(target.anchor) : null) ??
                (target.locator ? locateInMounted(target.locator) : null)
            if (el) scrollToElement(el)
            else if (target.anchor) state.notice = 'That link points somewhere unavailable.'
        }

        settleTransition(token)
    }

    /** Outside the flow, keeping the passage it was opened from for the way
     * back. */
    async function mountStandalone(
        href: string,
        fragment: string,
        token: number
    ): Promise<boolean> {
        if (!isCurrent(token)) return false
        if (!state.standalone) {
            const passage = captureLocator() ?? pendingLocator
            standaloneReturn = { pageIndex: state.pageIndex, locator: passage }
            snapshotPassage()
        }
        const source = structure()
        const index = spineIndexOf(source, href)
        state.restoring = true
        captureEnabled = false

        let next: MountedSlice[] | null = null
        try {
            next = await buildSlices([{ href, spineIndex: index, start: null, end: null }], token)
        } catch {
            next = null
        }
        if (!isCurrent(token)) return false
        if (!next) {
            state.notice = 'That part of the book is unavailable.'
            return false
        }

        commit(next, {
            kind: 'standalone',
            href,
            title: source.spine[index]?.title || href,
        })
        scrollWindowTo(0)
        const el = fragment ? locateAnchor({ href, fragment }) : null
        if (el) scrollToElement(el)
        settleTransition(token)
        return true
    }

    function captureLocator(): BookLocator | null {
        if (state.standalone) return null
        const top = getLayoutTop()
        for (const slice of mounted) {
            const el = firstVisibleBlock(slice.root, top)
            if (!el) continue
            return {
                version: 1,
                href: slice.slice.href,
                textOffset: slice.startOffset + textOffsetOfNode(slice.root, el),
                anchorId: precedingAnchorId(el, slice.root),
            }
        }
        return null
    }

    function clearStampTimer() {
        if (stampTimer) clearTimeout(stampTimer)
        stampTimer = null
    }

    /** Rate-limited so scrolling can't flood `replaceState`, but with a
     * trailing stamp so the last position is never the one left out. */
    function stampLocator(locator: BookLocator, force = false) {
        if (disposed) return
        const now = Date.now()
        const waited = now - lastStamp
        if (force || waited >= HISTORY_STAMP_INTERVAL) {
            clearStampTimer()
            lastStamp = now
            nav.saveLocator(locator)
            return
        }
        if (stampTimer) return
        stampTimer = setTimeout(() => {
            stampTimer = null
            if (pendingLocator) stampLocator(pendingLocator, true)
        }, HISTORY_STAMP_INTERVAL - waited)
    }

    function snapshotPassage() {
        captureNow()
        if (pendingLocator) stampLocator(pendingLocator, true)
        persist.flush()
    }

    function setLocator(locator: BookLocator, stamp: boolean) {
        pendingLocator = locator
        if (stamp) stampLocator(locator)
        const doc = prepared.get(locator.href)
        const fraction = doc?.textLength ? locator.textOffset / doc.textLength : 0
        state.percent = progressPercent(weights, spineIndexOf(structure(), locator.href), fraction)
        void persist()
    }

    function captureNow(stamp = true) {
        if (!captureEnabled || state.standalone || !state.structure) return
        const locator = captureLocator()
        if (locator) setLocator(locator, stamp)
    }

    const captureSoon = useDebounceFn(() => {
        if (!disposed) captureNow()
    }, CAPTURE_DEBOUNCE)

    function nextStatus(): ReadingStatus | undefined {
        if (userData?.status && userData.status !== 'reading') return undefined
        return completed ? 'completed' : 'reading'
    }

    function write(final = false) {
        if (!state.content || !pendingLocator || closing) return
        const locator = pendingLocator
        const payload = {
            status: nextStatus(),
            progress: {
                ...(userData?.progress ?? {}),
                book: locator,
                progress_percent: state.percent,
            },
        }
        if (final) {
            closing = true
            // An ordinary write already in flight could otherwise commit after
            // this one and resurrect an older passage.
            writeController?.abort()
            const request = contentApi
                .updateUserData(contentId, payload, { keepalive: true })
                .catch(err => {
                    console.error('Failed to update reading progress', err)
                })
            writeChain = Promise.allSettled([writeChain, request]).then(() => {})
            return
        }

        writeChain = writeChain
            .then(async () => {
                if (closing) return
                writeController = new AbortController()
                userData = await contentApi.updateUserData(contentId, payload, {
                    signal: writeController.signal,
                })
                writeController = null
                queryClient.invalidateQueries({ queryKey: ['content', contentId] })
            })
            .catch(err => {
                if (!closing) console.error('Failed to update reading progress', err)
            })
    }

    const persist = useDebounceFn(() => write(), PERSIST_DEBOUNCE)

    /** The debounce alone loses the last interval on a reload or a tab close,
     * so the closing write goes out with `keepalive`. */
    function flushProgress(final = false, stamp = true) {
        captureNow(stamp)
        if (final) {
            persist.cancel()
            write(true)
        } else {
            persist.flush()
        }
    }

    function checkCompletion() {
        if (completed || state.standalone || !inputSincePage || !sentinel) return
        if (state.pageIndex !== state.pages.length - 1) return
        const rect = sentinel.getBoundingClientRect()
        if (rect.bottom <= 0 || rect.top >= window.innerHeight) return
        completed = true
        void persist()
    }

    function onScroll() {
        if (programmaticScrolls > 0) return
        if (state.restoring) restoreCancelled = true
        if (!captureEnabled) return
        void captureSoon()
        checkCompletion()
    }

    function onInput() {
        if (state.restoring) return
        inputSincePage = true
        checkCompletion()
    }

    function onVisibility() {
        if (document.visibilityState === 'visible') {
            closing = false
            return
        }
        flushProgress(true)
    }

    const onResizeEnd = useDebounceFn(() => {
        const locator = resizeLocator
        resizeLocator = null
        if (disposed || !locator || state.restoring) return
        const el = locateInMounted(locator)
        if (el) scrollToElement(el)
    }, RESIZE_DEBOUNCE)

    function onResize() {
        if (!captureEnabled) return
        resizeLocator ??= captureLocator()
        void onResizeEnd()
    }

    function onClick(event: MouseEvent) {
        if (event.defaultPrevented || event.button !== 0) return
        if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
        const anchor = event
            .composedPath()
            .find(
                (node): node is HTMLElement =>
                    node instanceof HTMLElement &&
                    (hasAttr(node, 'data-book-href') || hasAttr(node, 'data-book-missing'))
            )
        if (!anchor) return
        event.preventDefault()
        const href = getAttr(anchor, 'data-book-href')
        if (!href) {
            state.notice = 'That link points outside this book.'
            return
        }
        void followLink(href, getAttr(anchor, 'data-book-frag') ?? '')
    }

    /** Routes through the target's own index: the same href can live on
     * another page than the one the link sits on. */
    async function followLink(href: string, fragment: string) {
        const token = cancelPending()
        const source = structure()
        const index = spineIndexOf(source, href)

        if (index === -1 || !source.spine[index]!.linear) {
            if (!(await mountStandalone(href, fragment, token))) settleTransition(token)
            return
        }

        const doc = await preparedFor(href)
        if (!isCurrent(token)) return
        if (!doc) {
            state.notice = 'That part of the book is missing.'
            settleTransition(token)
            return
        }
        const found = fragment ? !!resolveTargetElement(doc.doc, fragment) : false
        state.notice = fragment && !found ? 'That link points somewhere unavailable.' : null

        if (found && !state.standalone) {
            const el = locateAnchor({ href, fragment })
            if (el) {
                scrollToElement(el)
                settleTransition(token)
                return
            }
        }

        const position = await positionFor(href, found ? fragment : '')
        if (!isCurrent(token)) return
        const page = position ? pageIndexForPosition(state.pages, position) : -1
        if (!position || page === -1) {
            settleTransition(token)
            return
        }

        const anchor = found ? { href, fragment } : null
        const destination = anchor ?? { href, fragment: '' }
        // A link naming the entry we are already on gets no route change, so
        // this navigation has to finish the transition itself.
        if (
            entryKey({ ch: destination.href, frag: destination.fragment || null }) ===
            currentEntryKey
        ) {
            if (page === state.pageIndex && mounted.length && !state.standalone) {
                const el =
                    (anchor ? locateAnchor(anchor) : null) ??
                    locateInMounted(positionLocator(position))
                if (el) scrollToElement(el)
                settleTransition(token)
            } else {
                await navigateTo(
                    page,
                    { anchor, locator: anchor ? null : positionLocator(position) },
                    token
                )
            }
            return
        }

        snapshotPassage()
        nav.push(destination)
    }

    function onPageHide() {
        flushProgress(true)
    }

    const inputEvents = ['pointerdown', 'keydown', 'wheel', 'touchstart'] as const
    window.addEventListener('scroll', onScroll, { passive: true })
    window.addEventListener('resize', onResize)
    window.addEventListener('pagehide', onPageHide)
    document.addEventListener('visibilitychange', onVisibility)
    for (const name of inputEvents) window.addEventListener(name, onInput, { passive: true })

    const page = computed(() => state.pages[state.pageIndex] ?? null)

    void init()

    return reactive({
        ...toRefs(state),
        page,
        nextPage: computed(() => state.pages[state.pageIndex + 1] ?? null),
        prevPage: computed(() => state.pages[state.pageIndex - 1] ?? null),
        title: computed(() => state.standalone?.title ?? page.value?.title ?? ''),

        setElements(elements: { host: HTMLElement | null; sentinel: HTMLElement | null }) {
            host = elements.host
            sentinel = elements.sentinel
            if (!host) return
            if (mounted.length) host.replaceChildren(...mounted.map(slice => slice.holder))
            while (hostWaiters.length) hostWaiters.pop()!()
        },

        setEntry(next: BookEntry) {
            if (entryKey(next) === currentEntryKey) return
            currentEntryKey = entryKey(next)
            // The history entry has already moved, so this passage belongs to
            // the one we are leaving and must not be stamped onto it.
            flushProgress(false, false)
            if (!state.pages.length) {
                pendingEntry = next
                return
            }
            void applyEntry(next, begin(), false)
        },

        snapshotPassage,

        goToPage(index: number) {
            const target = state.pages[index]?.target
            if (!target) return
            snapshotPassage()
            cancelPending()
            nav.push(target)
        },

        async closeStandalone() {
            const token = begin()
            const back = standaloneReturn
            await navigateTo(back?.pageIndex ?? state.pageIndex, { locator: back?.locator }, token)
            if (isCurrent(token)) canonicalize(null)
        },

        dismissNotice() {
            state.notice = null
        },

        dispose() {
            disposed = true
            navToken++
            controller.abort()
            window.removeEventListener('scroll', onScroll)
            window.removeEventListener('resize', onResize)
            window.removeEventListener('pagehide', onPageHide)
            document.removeEventListener('visibilitychange', onVisibility)
            for (const name of inputEvents) window.removeEventListener(name, onInput)
            while (hostWaiters.length) hostWaiters.pop()!()
            clearStampTimer()
            flushProgress(true, false)
            captureEnabled = false
            captureSoon.cancel()
            onResizeEnd.cancel()
            resizeLocator = null
            return writeChain
        },
    })
}

export type BookSession = ReturnType<typeof createBookSession>
