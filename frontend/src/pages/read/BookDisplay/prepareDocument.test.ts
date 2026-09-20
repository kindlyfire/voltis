import { describe, expect, it } from 'vitest'
import { resolveTargetElement } from './buildPages'
import { docBody, findTarget } from './domSafe'
import { mountTree, prepareDocument, type PrepareContext } from './prepareDocument'

const SPINE = new Set(['OPS/text/ch1.xhtml', 'OPS/ch2.xhtml'])

function ctx(resources: Record<string, string> = {}): PrepareContext {
    return {
        contentId: 'c_1',
        mtime: '2026-01-01',
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
    it('rewrites img src, srcset and inline background urls', async () => {
        const html = `<body>
            <img id="i" src="../img/cover.png" srcset="../img/a.png 1x, ../img/b.png 2x" />
            <div id="d" style="background-image: url('../img/bg.jpg')"></div>
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

    it('rewrites SVG image refs', async () => {
        const html = `<body><svg><image id="im" xlink:href="../img/a.png" /><image id="im2" href="../img/b.png" /></svg></body>`
        const prepared = await prepareDocument(html, BASE, ctx())
        expect(pathOf(prepared.doc.getElementById('im')!.getAttribute('xlink:href'))).toBe(
            'OPS/img/a.png'
        )
        expect(pathOf(prepared.doc.getElementById('im2')!.getAttribute('href'))).toBe(
            'OPS/img/b.png'
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

describe('concurrent renders', () => {
    it('prepares documents in parallel without leaking rewriting state', async () => {
        const first = prepareDocument(
            `<body><img id="i" src="a.png" /></body>`,
            'OPS/one/x.xhtml',
            ctx()
        )
        const second = prepareDocument(
            `<body><img id="i" src="a.png" /></body>`,
            'OPS/two/y.xhtml',
            ctx()
        )
        const [a, b] = await Promise.all([first, second])
        expect(pathOf(a.doc.getElementById('i')!.getAttribute('src'))).toBe('OPS/one/a.png')
        expect(pathOf(b.doc.getElementById('i')!.getAttribute('src'))).toBe('OPS/two/a.png')
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

    it('rewrites uppercase URL() in inline styles', async () => {
        const html = `<body><div id="d" style="background: URL(../img/a.png)"></div></body>`
        const prepared = await prepareDocument(html, BASE, ctx())
        expect(prepared.doc.getElementById('d')!.getAttribute('style')).toContain(
            'path=OPS%2Fimg%2Fa.png'
        )
    })

    it("rewrites a link element's own inline style", async () => {
        const html = `<body><a id="a" href="#x" style="background: url(../img/a.png)">x</a></body>`
        const prepared = await prepareDocument(html, BASE, ctx())
        expect(prepared.doc.getElementById('a')!.getAttribute('style')).toContain(
            'path=OPS%2Fimg%2Fa.png'
        )
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
    it('rewrites each sheet once, so imported urls are not double-encoded', async () => {
        const html = `<html><head><link rel="stylesheet" href="../css/main.css" /></head><body><p>x</p></body></html>`
        const prepared = await prepareDocument(
            html,
            BASE,
            ctx({
                'OPS/css/main.css': `@import "sub/other.css";\np { background: url(own.png); }`,
                'OPS/css/sub/other.css': `p { background: url(img.png); }`,
            })
        )
        const css = prepared.styles.join('\n')
        expect(css).toContain('path=OPS%2Fcss%2Fsub%2Fimg.png')
        expect(css).toContain('path=OPS%2Fcss%2Fown.png')
        expect(css).not.toContain('path%3D')
    })

    it('keeps media, supports and layer conditions on an inlined import', async () => {
        const html = `<html><head><link rel="stylesheet" href="../css/main.css" /></head><body></body></html>`
        const prepared = await prepareDocument(
            html,
            BASE,
            ctx({
                'OPS/css/main.css': `@IMPORT url("print.css") layer(book) supports(display: grid) print and (min-width: 10px);`,
                'OPS/css/print.css': `p { color: red }`,
            })
        )
        const css = prepared.styles.join('\n')
        expect(css).toContain('@layer book')
        expect(css).toContain('@supports (display: grid)')
        expect(css).toContain('@media print and (min-width: 10px)')
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
})

describe('document clobbering', () => {
    function mount(prepared: Awaited<ReturnType<typeof prepareDocument>>) {
        const host = document.createElement('div')
        const body = docBody(prepared.doc)!.cloneNode(true) as Element
        return { mounted: mountTree(host, prepared, body), shadow: host.shadowRoot! }
    }

    it('survives an element that shadows createTreeWalker', async () => {
        const html = `<html><body><img name="createTreeWalker" src="../img/a.png" /><p id="p">text</p></body></html>`
        const prepared = await prepareDocument(html, BASE, ctx())
        expect(prepared.textLength).toBe('text'.length)
        const { mounted } = mount(prepared)
        expect(mounted.querySelector('#p')!.textContent).toBe('text')
    })

    it('reads the real root when an element shadows documentElement', async () => {
        const html = `<html lang="en"><body><img name="documentElement" lang="wrong" /><p id="p">x</p></body></html>`
        const prepared = await prepareDocument(html, BASE, ctx())
        expect(Object.fromEntries(prepared.rootAttributes).lang).toBe('en')
        const { shadow } = mount(prepared)
        expect(shadow.querySelector('html')!.getAttribute('lang')).toBe('en')
    })

    it('still finds targets when an element shadows querySelectorAll', async () => {
        const html = `<html><body><img name="querySelectorAll" /><p id="target">x</p></body></html>`
        const prepared = await prepareDocument(html, BASE, ctx())
        expect(resolveTargetElement(prepared.doc, 'target')).not.toBeNull()
    })

    it('still finds the body when an element shadows it', async () => {
        const html = `<html><body><img name="body" /><p id="p">body text</p></body></html>`
        const prepared = await prepareDocument(html, BASE, ctx())
        expect(docBody(prepared.doc)!.tagName.toLowerCase()).toBe('body')
        expect(prepared.textLength).toBe('body text'.length)
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

    it('keeps a nested supports() condition out of the media query', async () => {
        const html = `<html><head><link rel="stylesheet" href="../css/main.css" /></head><body></body></html>`
        const prepared = await prepareDocument(
            html,
            BASE,
            ctx({
                'OPS/css/main.css': `@import "a.css" supports(selector(:is(h1,h2))) screen and (min-width: 10px);`,
                'OPS/css/a.css': `h1 { color: red }`,
            })
        )
        const css = prepared.styles.join('\n')
        expect(css).toContain('@supports (selector(:is(h1,h2)))')
        expect(css).toContain('@media screen and (min-width: 10px)')
        expect(css).not.toContain('@media supports')
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
