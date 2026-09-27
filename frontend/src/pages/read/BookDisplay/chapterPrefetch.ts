import type { BookChapter, ChapterSlice } from './buildChapters'
import { pathInRange, pathOfNode } from './documentRange'
import { docBody, elementsUnder, getAttr, tagOf } from './domSafe'
import type { PreparedDocument } from './prepareDocument'

const MAX_IMAGES = 8
const MAX_CONCURRENT = 3
const IDLE_FALLBACK = 300

interface ImageSource {
    src: string | null
    srcset: string | null
    sizes: string | null
}

function isData(url: string | null) {
    return !!url && /^\s*data:/i.test(url)
}

/** The first images of `slice` in document order, as the mounted slice will
 * request them. */
export function sliceImages(prepared: PreparedDocument, slice: ChapterSlice): ImageSource[] {
    const body = docBody(prepared.doc)
    if (!body) return []
    const out: ImageSource[] = []
    for (const el of elementsUnder(body)) {
        if (out.length >= MAX_IMAGES) break
        const tag = tagOf(el)
        if (tag !== 'img' && tag !== 'image') continue
        const path = pathOfNode(body, el)
        if (!path || !pathInRange(path, slice.start, slice.end)) continue
        const source =
            tag === 'img'
                ? {
                      src: getAttr(el, 'src'),
                      srcset: getAttr(el, 'srcset'),
                      sizes: getAttr(el, 'sizes'),
                  }
                : {
                      src: getAttr(el, 'href') ?? getAttr(el, 'xlink:href'),
                      srcset: null,
                      sizes: null,
                  }
        if (isData(source.src) || (!source.src && !source.srcset)) continue
        out.push(source)
    }
    return out
}

/** Readies the chapters on either side of the current one while the reader is
 * idle: the next one's first document and its first images, which a forward
 * turn needs at once, and the previous one's last document. Only the edge
 * documents: preparing a very large one blocks for about 100 ms. */
export function createChapterPrefetch(prepare: (href: string) => Promise<PreparedDocument>) {
    let idle: number | null = null
    let timer: ReturnType<typeof setTimeout> | null = null
    let generation = 0

    function cancel() {
        generation++
        if (idle != null) cancelIdleCallback(idle)
        if (timer) clearTimeout(timer)
        idle = null
        timer = null
    }

    function warm(sources: ImageSource[], live: () => boolean) {
        const queue = [...sources]
        const next = () => {
            const source = queue.shift()
            if (!source || !live()) return
            // Only fills the HTTP cache, which versioned resources may use.
            const img = new Image()
            img.onload = img.onerror = next
            // In this order, so the candidate chosen is the one the mounted
            // element will choose.
            if (source.sizes) img.sizes = source.sizes
            if (source.srcset) img.srcset = source.srcset
            if (source.src) img.src = source.src
        }
        for (let i = 0; i < MAX_CONCURRENT; i++) next()
    }

    async function run(
        next: BookChapter | null,
        previous: BookChapter | null,
        current: () => boolean
    ) {
        const own = generation
        const live = () => own === generation && current()
        const first = next?.slices[0]
        if (first) {
            const prepared = await prepare(first.href).catch(() => null)
            if (prepared && live()) warm(sliceImages(prepared, first), live)
        }
        const last = previous?.slices.at(-1)
        if (last && live()) await prepare(last.href).catch(() => null)
    }

    return {
        schedule(next: BookChapter | null, previous: BookChapter | null, current: () => boolean) {
            cancel()
            if (!next && !previous) return
            const job = () => {
                idle = null
                timer = null
                if (current()) void run(next, previous, current)
            }
            if (typeof requestIdleCallback === 'function') idle = requestIdleCallback(job)
            else timer = setTimeout(job, IDLE_FALLBACK)
        },
        cancel,
    }
}
