import { docBody } from './domSafe'
import { PAGED_CSS } from './pagedCss'
import { mountTree, prepareDocument, type PrepareContext } from './prepareDocument'

/** Real slices in a real paged frame, for the browser tests. Monospace at a
 * fixed size keeps line and column counts stable across engines. */
export interface FixtureDoc {
    href: string
    body: string
    css?: string
}

export interface Fixture {
    reader: HTMLElement
    viewport: HTMLElement
    host: HTMLElement
    holders: HTMLElement[]
    roots: Element[]
    dispose(): void
}

const BASE_CSS = `body { margin: 0 } p { margin: 0 }`

export function words(n: number, prefix = 'w') {
    return Array.from({ length: n }, (_, i) => `${prefix}${i}`).join(' ')
}

/** The reader, viewport and host as `BookReader.vue` renders them, paged. */
export function mountReader() {
    const style = document.createElement('style')
    style.textContent = PAGED_CSS
    document.head.append(style)

    const reader = document.createElement('div')
    reader.className = 'book-reader is-paged'
    reader.style.cssText = `display: flex; flex-direction: column;
        --reader-font-family: monospace; --reader-font-size: 16px; --reader-line-height: 20px;`
    const viewport = document.createElement('div')
    viewport.className = 'book-viewport'
    const host = document.createElement('div')
    host.className = 'book-host'
    viewport.append(host)
    reader.append(viewport)
    document.body.style.margin = '0'
    document.body.append(reader)
    return {
        reader,
        viewport,
        host,
        dispose() {
            reader.remove()
            style.remove()
        },
    }
}

export async function mountFixture(docs: FixtureDoc[]): Promise<Fixture> {
    const { reader, viewport, host, dispose } = mountReader()
    const sheets: Record<string, string> = {}
    const ctx: PrepareContext = {
        contentId: 'c_test',
        version: null,
        spineHrefs: new Set(docs.map(doc => doc.href)),
        readerPath: '/r/c_test',
        fetchText: async path => sheets[path] ?? '',
    }
    const holders: HTMLElement[] = []
    const roots: Element[] = []
    for (const doc of docs) {
        const sheet = `${doc.href}.css`
        sheets[sheet] = BASE_CSS + (doc.css ?? '')
        const html = `<html><head><link rel="stylesheet" href="${sheet}"></head><body>${doc.body}</body></html>`
        const prepared = await prepareDocument(html, doc.href, ctx)
        const holder = document.createElement('div')
        roots.push(mountTree(holder, prepared, docBody(prepared.doc)!.cloneNode(true) as Element))
        holders.push(holder)
    }
    host.append(...holders)

    return { reader, viewport, host, holders, roots, dispose }
}
