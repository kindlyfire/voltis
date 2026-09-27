import { describe, expect, it } from 'vitest'
import type { Content } from '@/utils/api/types'
import { resolveTargetElement } from './buildChapters'
import { docBody } from './domSafe'
import {
    fileVersion,
    mountTree,
    prepareDocument,
    setReaderDark,
    updateUserStyles,
    type PreparedDocument,
} from './prepareDocument'

const BASE = 'OPS/text/ch1.xhtml'

/** Prepares `html` as `base`; `resources` are the fetchable files (anything else fails). */
function prep(html: string, resources: Record<string, string> = {}, base = BASE) {
    return prepareDocument(html, base, {
        contentId: 'c_1',
        version: '2026-01-01',
        spineHrefs: new Set(['OPS/text/ch1.xhtml', 'OPS/ch2.xhtml']),
        readerPath: '/r/c_1',
        fetchText: async path => {
            const found = resources[path]
            if (found === undefined) throw new Error(`missing ${path}`)
            return found
        },
    })
}

/** The composed CSS of a chapter linking `OPS/css/main.css`; sheets are named relative to `OPS/css/`. */
async function linkedCss(sheets: Record<string, string>) {
    const html = `<html><head><link rel="stylesheet" href="../css/main.css" /></head><body></body></html>`
    const resources = Object.fromEntries(
        Object.entries(sheets).map(([k, v]) => [`OPS/css/${k}`, v])
    )
    const prepared = await prep(html, resources)
    expect(prepared.doc.querySelector('link')).toBeNull()
    return prepared.styles.join('\n')
}

function mounted(prepared: PreparedDocument, publisherFonts?: boolean) {
    const host = document.createElement('div')
    const body = docBody(prepared.doc)!.cloneNode(true) as Element
    return { host, body: mountTree(host, prepared, body, publisherFonts), shadow: host.shadowRoot! }
}

function pathOf(url: string | null | undefined): string | null {
    if (!url) return null
    return new URLSearchParams(url.slice(url.indexOf('?') + 1)).get('path')
}

describe('sanitization', () => {
    it('keeps ids that collide with document properties, and legacy anchors', async () => {
        const { doc } = await prep(`<html><body>
            <h1 id="title">T</h1>
            <p id="body">b</p>
            <div id="location"><span id="forms">f</span></div>
            <a name="anchor-1">legacy</a>
            <p id="all">a</p>
        </body></html>`)
        for (const id of ['title', 'body', 'location', 'forms', 'all']) {
            expect(doc.getElementById(id), id).not.toBeNull()
        }
        expect(doc.querySelector('[name="anchor-1"]')).not.toBeNull()
    })

    it('still strips scripts and event handlers', async () => {
        const { doc } = await prep(
            `<body><p id="p1" onclick="alert(1)">x</p><script>alert(2)</script></body>`
        )
        expect(doc.querySelector('script')).toBeNull()
        expect(doc.getElementById('p1')!.hasAttribute('onclick')).toBe(false)
    })

    it('strips form elements, which are the DOM-clobbering vector', async () => {
        const { doc } = await prep(`<body>
            <form action="/api/steal"><input name="getAttribute" /><button name="children">go</button></form>
            <p id="after">still here</p>
        </body>`)
        expect(doc.querySelector('form, input, button')).toBeNull()
        expect(doc.getElementById('after')).not.toBeNull()
    })

    it('reads the real document through elements that shadow its members', async () => {
        const prepared = await prep(`<html lang="en"><body>
            <img name="createTreeWalker" src="../img/a.png" />
            <img name="documentElement" lang="wrong" />
            <img name="querySelectorAll" />
            <img name="body" />
            <p id="target">text</p>
        </body></html>`)
        expect(prepared.textLength).toBe('text'.length)
        expect(Object.fromEntries(prepared.rootAttributes).lang).toBe('en')
        expect(resolveTargetElement(prepared.doc, 'target')).not.toBeNull()
        expect(docBody(prepared.doc)!.tagName.toLowerCase()).toBe('body')

        const { shadow } = mounted(prepared)
        expect(shadow.querySelector('html')!.getAttribute('lang')).toBe('en')
        expect(shadow.querySelector('#target')!.textContent).toBe('text')
    })
})

describe('resource rewriting', () => {
    it('rewrites img src, srcset and inline background urls, including on a link', async () => {
        const { doc } = await prep(`<body>
            <img id="i" src="../img/cover.png" srcset="../img/a.png 1x, ../img/b.png 2x" />
            <a id="d" href="#local" style="background-image: URL('../img/bg.jpg')">x</a>
        </body>`)
        const img = doc.getElementById('i')!
        expect(pathOf(img.getAttribute('src'))).toBe('OPS/img/cover.png')
        const srcset = img.getAttribute('srcset')!.split(', ')
        expect(pathOf(srcset[0]!.split(' ')[0])).toBe('OPS/img/a.png')
        expect(srcset[0]!.endsWith(' 1x')).toBe(true)
        expect(pathOf(srcset[1]!.split(' ')[0])).toBe('OPS/img/b.png')
        expect(doc.getElementById('d')!.getAttribute('style')).toContain('path=OPS%2Fimg%2Fbg.jpg')
    })

    // From its own base, not the one the other tests use.
    it('rewrites SVG image refs', async () => {
        const { doc } = await prep(
            `<body><svg><image id="im" xlink:href="../img/a.png" /><image id="im2" href="../img/b.png" /></svg></body>`,
            {},
            'OPS/text/sub/deep.xhtml'
        )
        expect(pathOf(doc.getElementById('im')!.getAttribute('xlink:href'))).toBe(
            'OPS/text/img/a.png'
        )
        expect(pathOf(doc.getElementById('im2')!.getAttribute('href'))).toBe('OPS/text/img/b.png')
    })

    it('resolves stylesheet urls against the stylesheet, not the chapter', async () => {
        const css = await linkedCss({
            'main.css': `@import url("sub/other.css");\nbody { background: url(../img/paper.png); }`,
            'sub/other.css': `@font-face { src: url('fonts/serif.woff2') format("woff2"); }`,
        })
        expect(css).toContain('path=OPS%2Fimg%2Fpaper.png')
        expect(css).toContain('path=OPS%2Fcss%2Fsub%2Ffonts%2Fserif.woff2')
        expect(css).toContain('@font-face')
        expect(css).toContain('format("woff2")')
        // Each sheet is rewritten once: a second pass would encode the `?path=`.
        expect(css).not.toContain('path%3D')
    })

    it('drops a stylesheet that cannot be fetched without failing the document', async () => {
        const prepared = await prep(
            `<html><head><link rel="stylesheet" href="missing.css" /></head><body><p id="p">x</p></body></html>`
        )
        expect(prepared.styles).toEqual([])
        expect(prepared.doc.querySelector('link')).toBeNull()
        expect(prepared.doc.getElementById('p')).not.toBeNull()
    })

    it('removes references it will not rewrite instead of leaving them live', async () => {
        const { doc } = await prep(`<body>
            <img id="i1" src="../../../api/test?x=1" />
            <img id="i2" src="/etc/passwd" srcset="../../../escape.png 1x" />
            <div id="d1" style="background: url('../../../outside.png')"></div>
            <svg><image id="s1" xlink:href="..%2f..%2fsecret.png" /></svg>
        </body>`)
        expect(doc.getElementById('i1')!.hasAttribute('src')).toBe(false)
        expect(doc.getElementById('i2')!.hasAttribute('src')).toBe(false)
        expect(doc.getElementById('i2')!.hasAttribute('srcset')).toBe(false)
        expect(doc.getElementById('d1')!.getAttribute('style')).toContain('about:invalid')
        expect(doc.getElementById('s1')!.hasAttribute('xlink:href')).toBe(false)
    })

    it('keeps data: and same-document references', async () => {
        const { doc } = await prep(`<body>
            <img id="i" src="data:image/gif;base64,R0lGOD" />
            <div id="d" style="fill: url(#grad)"></div>
        </body>`)
        expect(doc.getElementById('i')!.getAttribute('src')).toContain('data:image/gif')
        expect(doc.getElementById('d')!.getAttribute('style')).toContain('url(#grad)')
    })
})

describe('link rewriting', () => {
    it('routes in-book links through the reader and marks the rest', async () => {
        const { doc } = await prep(`<body>
            <a id="a1" href="../ch2.xhtml#note-3">in book</a>
            <a id="a2" href="#local">same doc</a>
            <a id="a3" href="https://example.com">out</a>
            <a id="a4" href="../elsewhere.xhtml">off spine</a>
            <a id="a5" href="../images/pic.png">not a document</a>
        </body>`)
        const attr = (id: string, name: string) => doc.getElementById(id)!.getAttribute(name)
        expect(attr('a1', 'data-book-href')).toBe('OPS/ch2.xhtml')
        expect(attr('a1', 'data-book-frag')).toBe('note-3')
        expect(attr('a1', 'href')).toBe('/r/c_1?ch=OPS%2Fch2.xhtml&frag=note-3')
        expect(attr('a2', 'data-book-href')).toBe(BASE)
        expect(attr('a2', 'data-book-frag')).toBe('local')
        expect(attr('a3', 'href')).toBe('https://example.com')
        expect(attr('a3', 'target')).toBe('_blank')
        expect(attr('a4', 'data-book-href')).toBe('OPS/elsewhere.xhtml')
        expect(attr('a5', 'href')).toBeNull()
        expect(attr('a5', 'data-book-missing')).toBe('1')
    })
})

describe('stylesheet composition', () => {
    it('keeps media, supports and layer conditions on an inlined import', async () => {
        const css = await linkedCss({
            'main.css': `@IMPORT url("print.css") layer(book) supports(selector(:is(h1,h2))) print and (min-width: 10px);`,
            'print.css': `p { color: red }`,
        })
        expect(css).toContain('@layer book')
        expect(css).toContain('@supports (selector(:is(h1,h2)))')
        expect(css).toContain('@media print and (min-width: 10px)')
        expect(css).not.toContain('@media supports')
        expect(css).toContain('color: red')
    })

    // Cycle detection has to be per branch, not a single visited set.
    it('inlines the same sheet down two different branches', async () => {
        const css = await linkedCss({
            'main.css': `@import "screen.css" screen;\n@import "print.css" print;`,
            'screen.css': `@import "shared.css";`,
            'print.css': `@import "shared.css";`,
            'shared.css': `p { color: blue }`,
        })
        expect(css).toContain('@media screen')
        expect(css).toContain('@media print')
        expect(css.match(/color: blue/g)).toHaveLength(2)
    })

    it('drops an import it will not inline rather than leaking the request', async () => {
        const css = await linkedCss({
            'main.css': `@import url("https://cdn.example.com/x.css");\n@import "../../escape.css";\np { color: green }`,
        })
        expect(css).not.toContain('@import')
        expect(css).toContain('color: green')
    })

    it('inlines and rejects imports written with no separator', async () => {
        const css = await linkedCss({
            'main.css': `@import"sub.css";@import/**/"other.css";@import"/api/probe";p{color:red}`,
            'sub.css': `.a{color:blue}`,
            'other.css': `.b{color:green}`,
        })
        for (const color of ['blue', 'green', 'red']) expect(css).toContain(`color:${color}`)
        expect(css).not.toContain('@import')
        expect(css).not.toContain('/api/probe')
    })

    it('stops a self-importing cycle', async () => {
        const css = await linkedCss({
            'main.css': `@import "b.css";\np{color:red}`,
            'b.css': `@import "main.css";\np{color:blue}`,
        })
        expect(css).toContain('color:red')
        expect(css).toContain('color:blue')
        expect(css).not.toContain('@import')
    })

    it('strips an import that exceeds the depth limit', async () => {
        const sheets: Record<string, string> = { 'main.css': '@import "s1.css";' }
        for (let i = 1; i < 7; i++)
            sheets[`s${i}.css`] = `@import "s${i + 1}.css";\n.s${i} { color: red }`
        const css = await linkedCss(sheets)
        expect(css).not.toContain('@import')
        expect(css).toContain('.s4')
        expect(css).not.toContain('.s5')
    })

    it('honours the media attribute of a style element', async () => {
        const prepared = await prep(
            `<html><head><style media="print">p { color: red }</style></head><body></body></html>`
        )
        expect(prepared.styles[0]).toContain('@media print')
    })

    it('turns publisher page breaks into column breaks in stylesheets, imports and inline styles', async () => {
        const prepared = await prep(
            `<html><head>
                <link rel="stylesheet" href="../book.css">
                <style>h1 { page-break-before: always } .x { break-after: right }</style>
            </head><body>
                <h2 style="PAGE-BREAK-AFTER: always; color: red">t</h2>
                <p style="break-inside: avoid">p</p>
            </body></html>`,
            {
                'OPS/book.css': '@import "more.css"; .a { break-before: page }',
                'OPS/more.css': '.b { page-break-after: left } .c { page-break-inside: avoid }',
            }
        )
        const css = prepared.styles.join('\n')
        expect(css).toContain('.a { break-before: column }')
        expect(css).toContain('.b { break-after: column }')
        expect(css).toContain('page-break-inside: avoid')
        expect(css).toContain('h1 { break-before: column }')
        expect(css).toContain('.x { break-after: column }')
        const body = docBody(prepared.doc)!
        expect(body.querySelector('h2')!.getAttribute('style')).toBe(
            'break-after: column; color: red'
        )
        expect(body.querySelector('p')!.getAttribute('style')).toBe('break-inside: avoid')
    })
})

describe('layout tables', () => {
    it('marks a table of one plain cell, not lists or data tables', async () => {
        const one = (attrs = '') => `<tr><td${attrs}>x</td></tr>`
        const { doc } = await prep(`<html><body>
            <table id="wrap" class="note"><tr><td><p>blurb</p>
                <table id="data"><tr><td>a</td><td>b</td></tr></table>
                <table id="inner">${one()}</table>
            </td></tr></table>
            <table id="colspan"><tbody>${one(' colspan="2"')}</tbody></table>
            <table id="list">${one()}${one()}</table>
            <table id="th"><tr><th>h</th></tr></table>
            <table id="caption"><caption>c</caption>${one()}</table>
            <table id="empty"></table>
        </body></html>`)
        const marked = Array.from(doc.querySelectorAll('.book-layout-table'), el => el.id)
        expect(marked).toEqual(['wrap', 'inner', 'colspan'])
        expect(doc.getElementById('wrap')!.className).toBe('note book-layout-table')
    })

    it('lays out only the marked table own parts as blocks, paged only', async () => {
        const prepared = await prep(`<html><body>
            <table class="book-layout-table"><tr><td><table><tr><td>a</td><td>b</td></tr></table></td></tr></table>
        </body></html>`)
        const { shadow } = mounted(prepared)
        const user = new CSSStyleSheet()
        user.replaceSync(Array.from(shadow.querySelectorAll('style')).at(-1)!.textContent!)
        const rule = Array.from(user.cssRules as CSSRuleList)
            .map(rule => rule as CSSStyleRule)
            .find(rule => rule.style.getPropertyValue('display') === 'block')!
        expect(rule.style.getPropertyPriority('display')).toBe('important')
        expect(rule.style.getPropertyValue('height')).toBe('auto')
        expect(rule.style.getPropertyPriority('height')).toBe('important')
        const selectors = rule.selectorText.split(/,\s*(?=:host)/)
        expect(selectors.every(sel => sel.startsWith(':host(.is-paged) '))).toBe(true)
        const inHost = selectors.map(sel => sel.slice(':host(.is-paged) '.length)).join(', ')
        const [outer, inner] = Array.from(shadow.querySelectorAll('table'))
        for (const el of [
            outer!,
            ...outer!.querySelectorAll(
                ':scope > tbody, :scope > tbody > tr, :scope > tbody > tr > td'
            ),
        ]) {
            expect(el.matches(inHost), el.tagName).toBe(true)
        }
        for (const el of [inner!, ...inner!.querySelectorAll('tbody, tr, td')]) {
            expect(el.matches(inHost), el.tagName).toBe(false)
        }
    })
})

describe('mounting', () => {
    it('does not restore references that preparation stripped', async () => {
        const prepared = await prep(
            `<html lang="en" style="background: url('../../../probe.png')"><body class="calibre"><p>x</p></body></html>`
        )
        const { shadow, body } = mounted(prepared)
        const root = shadow.querySelector('html')!
        expect(root.getAttribute('style')).toContain('about:invalid')
        expect(root.getAttribute('style')).not.toContain('probe.png')
        expect(root.getAttribute('lang')).toBe('en')
        expect(body.getAttribute('class')).toBe('calibre')
    })

    it('puts the user stylesheet after the book stylesheets, and swaps it in place', async () => {
        const prepared = await prep(
            `<html><head><style>p { font-family: Papyrus }</style></head><body><p>x</p></body></html>`
        )
        const { host, shadow } = mounted(prepared)
        const sheets = () => Array.from(shadow.querySelectorAll('style'))
        const user = () => sheets().at(-1)!.textContent!
        expect(user()).toContain('font-family: inherit !important')
        expect(user()).toContain('font-size: inherit !important')
        expect(sheets().findIndex(sheet => sheet.textContent!.includes('Papyrus'))).toBeGreaterThan(
            0
        )

        const count = sheets().length
        updateUserStyles(host, true)
        expect(sheets()).toHaveLength(count)
        expect(user()).not.toContain('font-family')

        // Mounted with publisher fonts from the start: same sheet, no remount.
        const fresh = mounted(prepared, true).shadow
        const sheet = Array.from(fresh.querySelectorAll('style')).at(-1)!.textContent!
        expect(sheet).not.toContain('font-family')
        expect(sheet).toContain('font-size: inherit !important')
    })

    it("clears the book's html and body background color, not their image", async () => {
        const prepared = await prep(`<html><head><style>
            body { background: #fff url(../img/cover.jpg); margin: 2% }
            pre { background: #eee }
        </style></head><body><pre>code</pre></body></html>`)
        const { shadow } = mounted(prepared)
        const user = new CSSStyleSheet()
        user.replaceSync(Array.from(shadow.querySelectorAll('style')).at(-1)!.textContent!)
        const clear = Array.from(user.cssRules as CSSRuleList)
            .map(rule => rule as CSSStyleRule)
            .find(rule => rule.style.getPropertyValue('background-color') === 'transparent')!
        expect(clear.style.getPropertyPriority('background-color')).toBe('important')
        expect(clear.style.getPropertyValue('background-image')).toBe('')
        expect(shadow.querySelector('html')!.matches(clear.selectorText)).toBe(true)
        expect(shadow.querySelector('body')!.matches(clear.selectorText)).toBe(true)
        expect(shadow.querySelector('pre')!.matches(clear.selectorText)).toBe(false)
    })

    it('shares one dark-mode sheet that neutralizes every book color', async () => {
        const prepared = await prep(`<html><head><style>
            pre, table { background: #eee }
            .ink { color: #000 }
        </style></head><body>
            <pre>code</pre><table><tr><td>cell</td></tr></table>
            <p><span class="ink">inked</span> <font color="#000">old</font>
            <span style="color: #000; background: #fff">inline</span></p>
        </body></html>`)
        const [a, b] = [mounted(prepared).shadow, mounted(prepared).shadow]
        const sheet = a.adoptedStyleSheets[0]!
        expect(b.adoptedStyleSheets[0]).toBe(sheet)

        setReaderDark(true)
        const neutral = Array.from(sheet.cssRules as CSSRuleList)
            .map(rule => rule as CSSStyleRule)
            .find(rule => rule.style.getPropertyValue('background-color') === 'transparent')!
        expect(neutral.style.getPropertyPriority('color')).toBe('important')
        for (const selector of ['html', 'body', 'pre', 'table', 'td', '.ink', 'font', '[style]']) {
            expect(a.querySelector(selector)!.matches(neutral.selectorText), selector).toBe(true)
        }
        setReaderDark(false)
        expect(sheet.cssRules).toHaveLength(0)
    })
})

describe('fileVersion', () => {
    it('changes with the mtime or the size, as the scanner tells files apart', () => {
        const version = (file_mtime: string | null, file_size: number | null) =>
            fileVersion({ file_mtime, file_size } as Content)
        expect(version('2026-01-01', 10)).not.toBe(version('2026-01-02', 10))
        expect(version('2026-01-01', 10)).not.toBe(version('2026-01-01', 11))
        expect(version(null, 10)).toBeNull()
    })
})
