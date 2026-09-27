import { describe, expect, it } from 'vitest'
import type { Content } from '@/utils/api/types'
import { resolveTargetElement } from './buildChapters'
import { docBody, findTarget } from './domSafe'
import {
    fileVersion,
    mountTree,
    prepareDocument,
    setReaderDark,
    updateUserStyles,
    type PrepareContext,
} from './prepareDocument'

const SPINE = new Set(['OPS/text/ch1.xhtml', 'OPS/ch2.xhtml'])

function ctx(resources: Record<string, string> = {}): PrepareContext {
    return {
        contentId: 'c_1',
        version: '2026-01-01',
        spineHrefs: SPINE,
        readerPath: '/r/c_1',
        fetchText: async path => {
            const found = resources[path]
            if (found === undefined) throw new Error(`missing ${path}`)
            return found
        },
    }
}

const BASE = 'OPS/text/ch1.xhtml'

function pathOf(url: string | null | undefined): string | null {
    if (!url) return null
    return new URLSearchParams(url.slice(url.indexOf('?') + 1)).get('path')
}

describe('sanitization', () => {
    it('keeps ids that collide with document properties, and legacy anchors', async () => {
        const html = `<html><body>
            <h1 id="title">T</h1>
            <p id="body">b</p>
            <div id="location"><span id="forms">f</span></div>
            <a name="anchor-1">legacy</a>
            <p id="all">a</p>
        </body></html>`
        const prepared = await prepareDocument(html, BASE, ctx())
        const doc = prepared.doc
        for (const id of ['title', 'body', 'location', 'forms', 'all']) {
            expect(doc.getElementById(id), id).not.toBeNull()
        }
        expect(doc.querySelector('[name="anchor-1"]')).not.toBeNull()
    })

    it('still strips scripts and event handlers', async () => {
        const html = `<body><p id="p1" onclick="alert(1)">x</p><script>alert(2)</script></body>`
        const prepared = await prepareDocument(html, BASE, ctx())
        expect(prepared.doc.querySelector('script')).toBeNull()
        expect(prepared.doc.getElementById('p1')!.hasAttribute('onclick')).toBe(false)
    })
})

describe('resource rewriting', () => {
    it('rewrites img src, srcset and inline background urls, including on a link', async () => {
        const html = `<body>
            <img id="i" src="../img/cover.png" srcset="../img/a.png 1x, ../img/b.png 2x" />
            <a id="d" href="#local" style="background-image: URL('../img/bg.jpg')">x</a>
        </body>`
        const prepared = await prepareDocument(html, BASE, ctx())
        const img = prepared.doc.getElementById('i')!
        expect(pathOf(img.getAttribute('src'))).toBe('OPS/img/cover.png')
        const srcset = img.getAttribute('srcset')!.split(', ')
        expect(pathOf(srcset[0]!.split(' ')[0])).toBe('OPS/img/a.png')
        expect(srcset[0]!.endsWith(' 1x')).toBe(true)
        expect(pathOf(srcset[1]!.split(' ')[0])).toBe('OPS/img/b.png')
        const style = prepared.doc.getElementById('d')!.getAttribute('style')!
        expect(style).toContain('path=OPS%2Fimg%2Fbg.jpg')
    })

    // From its own base, not the one the other tests use.
    it('rewrites SVG image refs', async () => {
        const html = `<body><svg><image id="im" xlink:href="../img/a.png" /><image id="im2" href="../img/b.png" /></svg></body>`
        const prepared = await prepareDocument(html, 'OPS/text/sub/deep.xhtml', ctx())
        expect(pathOf(prepared.doc.getElementById('im')!.getAttribute('xlink:href'))).toBe(
            'OPS/text/img/a.png'
        )
        expect(pathOf(prepared.doc.getElementById('im2')!.getAttribute('href'))).toBe(
            'OPS/text/img/b.png'
        )
    })

    it('resolves stylesheet urls against the stylesheet, not the chapter', async () => {
        const html = `<html><head><link rel="stylesheet" href="../css/main.css" /></head><body><p>x</p></body></html>`
        const prepared = await prepareDocument(
            html,
            BASE,
            ctx({
                'OPS/css/main.css': `@import url("sub/other.css");\nbody { background: url(../img/paper.png); }`,
                'OPS/css/sub/other.css': `@font-face { src: url('fonts/serif.woff2') format("woff2"); }`,
            })
        )
        const css = prepared.styles.join('\n')
        expect(css).toContain('path=OPS%2Fimg%2Fpaper.png')
        expect(css).toContain('path=OPS%2Fcss%2Fsub%2Ffonts%2Fserif.woff2')
        expect(css).toContain('@font-face')
        expect(css).toContain('format("woff2")')
        // Each sheet is rewritten once: a second pass would encode the `?path=`.
        expect(css).not.toContain('path%3D')
        expect(prepared.doc.querySelector('link')).toBeNull()
    })

    it('drops a stylesheet that cannot be fetched without failing the document', async () => {
        const html = `<html><head><link rel="stylesheet" href="missing.css" /></head><body><p id="p">x</p></body></html>`
        const prepared = await prepareDocument(html, BASE, ctx())
        expect(prepared.styles).toEqual([])
        expect(prepared.doc.getElementById('p')).not.toBeNull()
    })
})

describe('link rewriting', () => {
    it('routes in-book links through the reader and marks the rest', async () => {
        const html = `<body>
            <a id="a1" href="../ch2.xhtml#note-3">in book</a>
            <a id="a2" href="#local">same doc</a>
            <a id="a3" href="https://example.com">out</a>
            <a id="a4" href="../elsewhere.xhtml">off spine</a>
            <a id="a5" href="../images/pic.png">not a document</a>
        </body>`
        const prepared = await prepareDocument(html, BASE, ctx())
        const a1 = prepared.doc.getElementById('a1')!
        expect(a1.getAttribute('data-book-href')).toBe('OPS/ch2.xhtml')
        expect(a1.getAttribute('data-book-frag')).toBe('note-3')
        expect(a1.getAttribute('href')).toBe('/r/c_1?ch=OPS%2Fch2.xhtml&frag=note-3')

        const a2 = prepared.doc.getElementById('a2')!
        expect(a2.getAttribute('data-book-href')).toBe(BASE)
        expect(a2.getAttribute('data-book-frag')).toBe('local')

        const a3 = prepared.doc.getElementById('a3')!
        expect(a3.getAttribute('href')).toBe('https://example.com')
        expect(a3.getAttribute('target')).toBe('_blank')

        const a4 = prepared.doc.getElementById('a4')!
        expect(a4.getAttribute('data-book-href')).toBe('OPS/elsewhere.xhtml')

        const a5 = prepared.doc.getElementById('a5')!
        expect(a5.hasAttribute('href')).toBe(false)
        expect(a5.getAttribute('data-book-missing')).toBe('1')
    })
})

describe('rejected references', () => {
    it('removes references it will not rewrite instead of leaving them live', async () => {
        const html = `<body>
            <img id="i1" src="../../../api/test?x=1" />
            <img id="i2" src="/etc/passwd" srcset="../../../escape.png 1x" />
            <div id="d1" style="background: url('../../../outside.png')"></div>
            <svg><image id="s1" xlink:href="..%2f..%2fsecret.png" /></svg>
        </body>`
        const prepared = await prepareDocument(html, BASE, ctx())
        expect(prepared.doc.getElementById('i1')!.hasAttribute('src')).toBe(false)
        expect(prepared.doc.getElementById('i2')!.hasAttribute('src')).toBe(false)
        expect(prepared.doc.getElementById('i2')!.hasAttribute('srcset')).toBe(false)
        expect(prepared.doc.getElementById('d1')!.getAttribute('style')).toContain('about:invalid')
        expect(prepared.doc.getElementById('s1')!.hasAttribute('xlink:href')).toBe(false)
    })

    it('keeps data: and same-document references', async () => {
        const html = `<body>
            <img id="i" src="data:image/gif;base64,R0lGOD" />
            <div id="d" style="fill: url(#grad)"></div>
        </body>`
        const prepared = await prepareDocument(html, BASE, ctx())
        expect(prepared.doc.getElementById('i')!.getAttribute('src')).toContain('data:image/gif')
        expect(prepared.doc.getElementById('d')!.getAttribute('style')).toContain('url(#grad)')
    })

    it('strips form elements, which are the DOM-clobbering vector', async () => {
        const html = `<body>
            <form action="/api/steal"><input name="getAttribute" /><button name="children">go</button></form>
            <p id="after">still here</p>
        </body>`
        const prepared = await prepareDocument(html, BASE, ctx())
        expect(prepared.doc.querySelector('form')).toBeNull()
        expect(prepared.doc.querySelector('input')).toBeNull()
        expect(prepared.doc.querySelector('button')).toBeNull()
        expect(prepared.doc.getElementById('after')).not.toBeNull()
    })
})

describe('stylesheet composition', () => {
    it('keeps media, supports and layer conditions on an inlined import', async () => {
        const html = `<html><head><link rel="stylesheet" href="../css/main.css" /></head><body></body></html>`
        const prepared = await prepareDocument(
            html,
            BASE,
            ctx({
                'OPS/css/main.css': `@IMPORT url("print.css") layer(book) supports(selector(:is(h1,h2))) print and (min-width: 10px);`,
                'OPS/css/print.css': `p { color: red }`,
            })
        )
        const css = prepared.styles.join('\n')
        expect(css).toContain('@layer book')
        expect(css).toContain('@supports (selector(:is(h1,h2)))')
        expect(css).toContain('@media print and (min-width: 10px)')
        expect(css).not.toContain('@media supports')
        expect(css).toContain('color: red')
    })

    // Two separate branches reach the same sheet: cycle detection has to be
    // per branch, not a single visited set.
    it('inlines the same sheet down two different branches', async () => {
        const html = `<html><head><link rel="stylesheet" href="../css/main.css" /></head><body></body></html>`
        const prepared = await prepareDocument(
            html,
            BASE,
            ctx({
                'OPS/css/main.css': `@import "screen.css" screen;\n@import "print.css" print;`,
                'OPS/css/screen.css': `@import "shared.css";`,
                'OPS/css/print.css': `@import "shared.css";`,
                'OPS/css/shared.css': `p { color: blue }`,
            })
        )
        const css = prepared.styles.join('\n')
        expect(css).toContain('@media screen')
        expect(css).toContain('@media print')
        expect(css.match(/color: blue/g)).toHaveLength(2)
    })

    it('drops an import it will not inline rather than leaking the request', async () => {
        const html = `<html><head><link rel="stylesheet" href="../css/main.css" /></head><body></body></html>`
        const prepared = await prepareDocument(
            html,
            BASE,
            ctx({
                'OPS/css/main.css': `@import url("https://cdn.example.com/x.css");\n@import "../../escape.css";\np { color: green }`,
            })
        )
        const css = prepared.styles.join('\n')
        expect(css).not.toContain('@import')
        expect(css).toContain('color: green')
    })

    it('honours the media attribute of a style element', async () => {
        const html = `<html><head><style media="print">p { color: red }</style></head><body></body></html>`
        const prepared = await prepareDocument(html, BASE, ctx())
        expect(prepared.styles[0]).toContain('@media print')
    })

    it('stops a self-importing cycle', async () => {
        const html = `<html><head><link rel="stylesheet" href="../css/a.css" /></head><body></body></html>`
        const prepared = await prepareDocument(
            html,
            BASE,
            ctx({
                'OPS/css/a.css': `@import "b.css";\np{color:red}`,
                'OPS/css/b.css': `@import "a.css";\np{color:blue}`,
            })
        )
        const css = prepared.styles.join('\n')
        expect(css).toContain('color:red')
        expect(css).toContain('color:blue')
        expect(css).not.toContain('@import')
    })

    it('strips an import that exceeds the depth limit', async () => {
        const sheets: Record<string, string> = {}
        for (let i = 0; i < 7; i++) {
            sheets[`OPS/css/s${i}.css`] = `@import "s${i + 1}.css";\n.s${i} { color: red }`
        }
        const html = `<html><head><link rel="stylesheet" href="../css/s0.css" /></head><body></body></html>`
        const prepared = await prepareDocument(html, BASE, ctx(sheets))
        const css = prepared.styles.join('\n')

        expect(css).not.toContain('@import')
        expect(css).toContain('.s0')
        expect(css).toContain('.s4')
        expect(css).not.toContain('.s5')
    })
})

describe('mounting', () => {
    it('does not restore references that preparation stripped', async () => {
        const html = `<html lang="en" style="background: url('../../../probe.png')"><body class="calibre"><p>x</p></body></html>`
        const prepared = await prepareDocument(html, BASE, ctx())
        const host = document.createElement('div')
        const body = docBody(prepared.doc)!.cloneNode(true) as Element
        const mounted = mountTree(host, prepared, body)

        const root = host.shadowRoot!.querySelector('html')!
        expect(root.getAttribute('style')).toContain('about:invalid')
        expect(root.getAttribute('style')).not.toContain('probe.png')
        expect(root.getAttribute('lang')).toBe('en')
        expect(mounted.getAttribute('class')).toBe('calibre')
    })

    it('puts the user stylesheet after the book stylesheets, and swaps it in place', async () => {
        const html = `<html><head><style>p { font-family: Papyrus }</style></head><body><p>x</p></body></html>`
        const prepared = await prepareDocument(html, BASE, ctx())
        const body = () => docBody(prepared.doc)!.cloneNode(true) as Element
        const host = document.createElement('div')
        mountTree(host, prepared, body())

        const sheets = () => Array.from(host.shadowRoot!.querySelectorAll('style'))
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
        const fresh = document.createElement('div')
        mountTree(fresh, prepared, body(), true)
        const sheet = Array.from(fresh.shadowRoot!.querySelectorAll('style')).at(-1)!.textContent!
        expect(sheet).not.toContain('font-family')
        expect(sheet).toContain('font-size: inherit !important')
    })

    it("clears the book's html and body background color, not their image", async () => {
        const html = `<html><head><style>
            body { background: #fff url(../img/cover.jpg); margin: 2% }
            pre { background: #eee }
        </style></head><body><pre>code</pre></body></html>`
        const prepared = await prepareDocument(html, BASE, ctx())
        const host = document.createElement('div')
        mountTree(host, prepared, docBody(prepared.doc)!.cloneNode(true) as Element)

        const root = host.shadowRoot!
        const user = new CSSStyleSheet()
        user.replaceSync(Array.from(root.querySelectorAll('style')).at(-1)!.textContent!)
        const clear = Array.from(user.cssRules as CSSRuleList)
            .map(rule => rule as CSSStyleRule)
            .find(rule => rule.style.getPropertyValue('background-color') === 'transparent')!
        expect(clear.style.getPropertyPriority('background-color')).toBe('important')
        expect(clear.style.getPropertyValue('background-image')).toBe('')
        expect(root.querySelector('html')!.matches(clear.selectorText)).toBe(true)
        expect(root.querySelector('body')!.matches(clear.selectorText)).toBe(true)
        expect(root.querySelector('pre')!.matches(clear.selectorText)).toBe(false)
    })

    it('shares one dark-mode sheet that neutralizes every book color', async () => {
        const html = `<html><head><style>
            pre, table { background: #eee }
            .ink { color: #000 }
        </style></head><body>
            <pre>code</pre><table><tr><td>cell</td></tr></table>
            <p><span class="ink">inked</span> <font color="#000">old</font>
            <span style="color: #000; background: #fff">inline</span></p>
        </body></html>`
        const prepared = await prepareDocument(html, BASE, ctx())
        const hosts = [0, 1].map(() => {
            const host = document.createElement('div')
            mountTree(host, prepared, docBody(prepared.doc)!.cloneNode(true) as Element)
            return host
        })
        const [a, b] = hosts.map(host => host.shadowRoot!.adoptedStyleSheets[0]!)
        expect(a).toBe(b)

        const text = () => Array.from(a!.cssRules, rule => rule.cssText).join('\n')
        setReaderDark(true)
        const neutral = Array.from(a!.cssRules as CSSRuleList)
            .map(rule => rule as CSSStyleRule)
            .find(rule => rule.style.getPropertyValue('background-color') === 'transparent')!
        expect(neutral.style.getPropertyPriority('color')).toBe('important')
        const root = hosts[0]!.shadowRoot!
        for (const selector of ['html', 'body', 'pre', 'table', 'td', '.ink', 'font', '[style]']) {
            expect(root.querySelector(selector)!.matches(neutral.selectorText), selector).toBe(true)
        }
        setReaderDark(false)
        expect(text()).toBe('')
    })
})

describe('document clobbering', () => {
    it('reads the real document through elements that shadow its members', async () => {
        const html = `<html lang="en"><body>
            <img name="createTreeWalker" src="../img/a.png" />
            <img name="documentElement" lang="wrong" />
            <img name="querySelectorAll" />
            <img name="body" />
            <p id="target">text</p>
        </body></html>`
        const prepared = await prepareDocument(html, BASE, ctx())
        expect(prepared.textLength).toBe('text'.length)
        expect(Object.fromEntries(prepared.rootAttributes).lang).toBe('en')
        expect(resolveTargetElement(prepared.doc, 'target')).not.toBeNull()
        expect(docBody(prepared.doc)!.tagName.toLowerCase()).toBe('body')

        const host = document.createElement('div')
        mountTree(host, prepared, docBody(prepared.doc)!.cloneNode(true) as Element)
        const shadow = host.shadowRoot!
        expect(shadow.querySelector('html')!.getAttribute('lang')).toBe('en')
        expect(shadow.querySelector('#target')!.textContent).toBe('text')
    })
})

describe('imports without whitespace', () => {
    it('inlines and rejects imports written with no separator', async () => {
        const html = `<html><head><link rel="stylesheet" href="../css/main.css" /></head><body></body></html>`
        const prepared = await prepareDocument(
            html,
            BASE,
            ctx({
                'OPS/css/main.css': `@import"sub.css";@import/**/"other.css";@import"/api/probe";p{color:red}`,
                'OPS/css/sub.css': `.a{color:blue}`,
                'OPS/css/other.css': `.b{color:green}`,
            })
        )
        const css = prepared.styles.join('\n')
        expect(css).toContain('color:blue')
        expect(css).toContain('color:green')
        expect(css).toContain('color:red')
        expect(css).not.toContain('@import')
        expect(css).not.toContain('/api/probe')
    })
})

describe('targets on the root element', () => {
    it('resolves a fragment naming the body itself', async () => {
        const html = `<html><body id="chapter"><p id="p">text</p></body></html>`
        const prepared = await prepareDocument(html, BASE, ctx())
        const body = docBody(prepared.doc)!
        expect(resolveTargetElement(prepared.doc, 'chapter')).toBe(body)
        expect(findTarget(body, 'p')).not.toBeNull()
    })
})

describe('publisher page breaks', () => {
    it('become column breaks in stylesheets, imports and inline styles', async () => {
        const html = `<html><head>
            <link rel="stylesheet" href="../book.css">
            <style>h1 { page-break-before: always } .x { break-after: right }</style>
        </head><body>
            <h2 style="PAGE-BREAK-AFTER: always; color: red">t</h2>
            <p style="break-inside: avoid">p</p>
        </body></html>`
        const prepared = await prepareDocument(
            html,
            BASE,
            ctx({
                'OPS/book.css': '@import "more.css"; .a { break-before: page }',
                'OPS/more.css': '.b { page-break-after: left } .c { page-break-inside: avoid }',
            })
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

describe('fileVersion', () => {
    const version = (file_mtime: string | null, file_size: number | null) =>
        fileVersion({ file_mtime, file_size } as Content)

    it('changes with the mtime or the size, as the scanner tells files apart', () => {
        expect(version('2026-01-01', 10)).not.toBe(version('2026-01-02', 10))
        expect(version('2026-01-01', 10)).not.toBe(version('2026-01-01', 11))
        expect(version(null, 10)).toBeNull()
    })
})
