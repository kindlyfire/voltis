import { useDebounceFn } from '@vueuse/core'
import {
    computed,
    markRaw,
    nextTick,
    reactive,
    shallowReactive,
    shallowRef,
    toRefs,
    watch,
    type Ref,
} from 'vue'
import { isNavigationFailure, type NavigationFailure } from 'vue-router'
import { contentApi, invalidateRecentlyRead, invalidateStatusChange } from '@/utils/api/content'
import type {
    BookLocator,
    BookStructure,
    Content,
    ReadingStatus,
    UserToContent,
} from '@/utils/api/types'
import { queryClient } from '@/utils/misc'
import { hasOpenModal } from '@/utils/modals'
import { chooseEntryChapter, entryKey, isBookLocator, type BookEntry } from './bookEntry'
import { flowWeights, progressPercent, type FlowWeights } from './bookProgress'
import type { BookSettings } from './bookSettings'
import {
    boundaryDocumentHrefs,
    buildChapters,
    mapEntriesToChapters,
    chapterIndexForPosition,
    chaptersContainingOffset,
    resolveFlowPosition,
    resolveTargetElement,
    spineIndexOf,
    type BookChapter,
    type FlowPosition,
    type ChapterSlice,
} from './buildChapters'
import { createChapterPrefetch } from './chapterPrefetch'
import { pathOfNode, pruneToRange, textOffsetAtPath } from './documentRange'
import { docBody, elementsUnder, findTarget, getAttr, hasAttr } from './domSafe'
import {
    fetchBookResource,
    fileVersion,
    mountTree,
    prepareDocument,
    updateUserStyles,
    type PrepareContext,
    type PreparedDocument,
} from './prepareDocument'
import {
    createReadingLayout,
    type Landing,
    type LayoutKind,
    type LayoutOptions,
    type ReadingLayout,
} from './readingLayout'

const SETTLE_TIMEOUT = 2000
const CAPTURE_DEBOUNCE = 250
const PERSIST_DEBOUNCE = 1000
const HISTORY_STAMP_INTERVAL = 500
/** Content that shows without any text. */
const VISIBLE_EMPTY = 'img, svg, video, picture, object, embed, hr, table, iframe'
const LOAD_FAILED = 'That part of the book could not be loaded.'

export interface BookAnchor {
    href: string
    fragment: string
}

export interface BookNav {
    push(target: BookAnchor): Promise<NavigationFailure | void | undefined>
    replace(target: BookAnchor): void
    /** Stores the reading position on the current history entry, so Back and
     * Forward return to the passage rather than the chapter top. */
    saveLocator(locator: BookLocator): void
    historyLocator(): BookLocator | null
    /** Runs before any router navigation moves off the current entry. */
    beforeLeave(callback: () => void): () => void
}

export interface BookSessionValues {
    contentId: string
    content: Content | null
    structure: BookStructure | null
    chapters: BookChapter[]
    entryChapters: Record<string, number>
    chapterIndex: number
    standalone: Standalone | null
    fallback: boolean
    loading: boolean
    error: string | null
    notice: string | null
    percent: number
    /** Latched when content first mounts; `loading` can clear before the first
     * chapter succeeds. */
    firstChapterMounted: boolean
}

/** A document outside the flow, and where closing it returns to. */
interface Standalone {
    href: string
    title: string
    returnTo: { chapterIndex: number; locator: BookLocator | null }
}

/** One navigation at a time: starting another supersedes it, and every await
 * checks `activity.value === mine` before acting. */
interface Navigation {
    phase: 'resolving' | 'routing' | 'loading' | 'settling'
    /** A routed navigation's entry, from its push until it commits. */
    entry: string | null
    /** Where it lands once settled; null after the reader moves meanwhile. */
    landing: Landing | null
}

type Activity = Navigation | { phase: 'ready' | 'book-end' | 'disposed' }

interface MountedSlice {
    slice: ChapterSlice
    prepared: PreparedDocument
    holder: HTMLElement
    root: Element
    startOffset: number
}

function delay(ms: number) {
    return new Promise(resolve => setTimeout(resolve, ms))
}

const BACKGROUND_IMAGE = /background(?:-image)?\s*:[^;}]*url\(/i

/** Errs towards showing: a cover or plate can be a bare element with a CSS
 * background image, which only styles can tell. */
export function isEmptySlice(body: Element, styles: string[]) {
    return (
        !/\S/.test(body.textContent ?? '') &&
        !body.querySelector(VISIBLE_EMPTY) &&
        ![body, ...elementsUnder(body)].some(el =>
            BACKGROUND_IMAGE.test(getAttr(el, 'style') ?? '')
        ) &&
        !styles.some(css => BACKGROUND_IMAGE.test(css))
    )
}

export function createBookSession(
    contentId: string,
    entry: BookEntry,
    nav: BookNav,
    settingsRef: Ref<BookSettings>
) {
    const settings = () => settingsRef.value
    const state = reactive<BookSessionValues>({
        contentId,
        content: null,
        structure: null,
        chapters: [],
        entryChapters: {},
        chapterIndex: 0,
        standalone: null,
        fallback: false,
        loading: true,
        error: null,
        notice: null,
        percent: 0,
        firstChapterMounted: false,
    })

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
    // Opening or restoring a passage isn't reading: only the user's own moves set a status.
    let navigated = false
    let inputSinceChapter = false
    let currentEntryKey = entryKey(entry)
    let lastStamp = 0
    let stampTimer: ReturnType<typeof setTimeout> | null = null
    let writeController: AbortController | null = null

    const activity = shallowRef<Activity>(
        shallowReactive({ phase: 'loading', entry: null, landing: null })
    )
    const restoring = computed(
        () => activity.value.phase === 'loading' || activity.value.phase === 'settling'
    )
    const hostWaiters: Array<() => void> = []
    const controller = new AbortController()
    const trees = new Map<string, Promise<PreparedDocument>>()
    const prepared = new Map<string, PreparedDocument>()
    const docs = new Map<string, Document>()
    const prefetch = createChapterPrefetch(prepare)

    function structure(): BookStructure {
        return state.structure!
    }

    function disposed() {
        return activity.value.phase === 'disposed'
    }

    function isCurrent(mine: Navigation) {
        return activity.value === mine
    }

    function start(phase: Navigation['phase'], landing: Landing | null = null): Navigation {
        clearStampTimer()
        const mine = shallowReactive<Navigation>({ phase, entry: null, landing })
        if (!disposed()) activity.value = mine
        return mine
    }

    /** Whatever is mounted now is what the reader has, whether this
     * navigation put it there or not. */
    function finish(mine: Navigation) {
        if (!isCurrent(mine)) return
        const ready = (activity.value = { phase: 'ready' })
        captureNow()
        if (!canCapture()) return
        prefetch.schedule(
            state.chapters[state.chapterIndex + 1] ?? null,
            state.chapters[state.chapterIndex - 1] ?? null,
            () => activity.value === ready
        )
    }

    /** A route change on its way: a turn would push again, or stamp the old
     * passage onto the new entry. */
    function crossing() {
        const current = activity.value
        return (
            current.phase === 'routing' || (current.phase === 'loading' && current.entry !== null)
        )
    }

    /** While a flow chapter is shown and no destination is being put on
     * screen, or the reader has taken over from one still settling. */
    function canCapture() {
        const current = activity.value
        const shown = current.phase === 'settling' ? current.landing === null : !restoring.value
        return shown && !disposed() && mounted.length > 0 && !state.standalone
    }

    function prepare(href: string): Promise<PreparedDocument> {
        const version = state.content && fileVersion(state.content)
        const key = `${href}@${version ?? ''}`
        let job = trees.get(key)
        if (!job) {
            job = contentApi
                .bookDocument(contentId, href, version, { signal: controller.signal })
                .then(html => prepareDocument(html, href, ctx!))
                .then(result => {
                    prepared.set(href, result)
                    docs.set(href, result.doc)
                    return result
                })
            trees.set(key, job)
            // A failure is not cached: the next navigation that needs it retries.
            job.catch(() => {
                if (trees.get(key) === job) trees.delete(key)
            })
        }
        return job
    }

    async function preparedFor(href: string): Promise<PreparedDocument | null> {
        return prepared.get(href) ?? (await prepare(href).catch(() => null))
    }

    function fallbackChapters(source: BookStructure): BookChapter[] {
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

    async function init(): Promise<boolean> {
        try {
            const [content, source] = await Promise.all([
                contentApi.get(contentId, { signal: controller.signal }),
                contentApi.bookStructure(contentId, { signal: controller.signal }),
            ])
            if (disposed()) return false

            state.content = content
            userData = content.user_data ?? null
            const version = fileVersion(content)
            ctx = {
                contentId,
                version,
                spineHrefs: new Set(source.spine.map(item => item.href)),
                readerPath: `/r/${contentId}`,
                fetchText: fetchBookResource(contentId, version),
                signal: controller.signal,
            }
            state.structure = markRaw(source)
            weights = flowWeights(source.spine)

            await Promise.all(
                boundaryDocumentHrefs(source).map(href => prepare(href).catch(() => null))
            )
            if (disposed()) return false

            const chapters = buildChapters(source, docs)
            state.fallback = chapters.length === 0
            state.chapters = markRaw(chapters.length ? chapters : fallbackChapters(source))
            state.loading = false
            if (!state.chapters.length) {
                state.error = 'This book has no readable content.'
                return false
            }
            state.entryChapters = Object.fromEntries(
                mapEntriesToChapters(source, state.chapters, docs)
            )
            return true
        } catch (err) {
            if (disposed()) return false
            console.error(err)
            state.error = err instanceof Error ? err.message : String(err)
            state.loading = false
            return false
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

    async function chapterForLocator(locator: BookLocator): Promise<number | null> {
        const source = structure()
        const spineIndex = spineIndexOf(source, locator.href)
        if (spineIndex === -1 || !source.spine[spineIndex]!.linear) return null
        const doc = await preparedFor(locator.href)
        if (!doc) return null

        const anchorChapter = () => {
            const body = docBody(doc.doc)!
            const el = locator.anchorId ? findTarget(body, locator.anchorId) : null
            const path = el ? pathOfNode(body, el) : null
            return path ? chapterIndexForPosition(state.chapters, { spineIndex, path }) : -1
        }

        if (locator.textOffset > doc.textLength) {
            const chapter = anchorChapter()
            return chapter === -1 ? null : chapter
        }

        // Textless or equal-offset intervals can't be told apart by offset
        // alone, so the anchor decides between them.
        const candidates = chaptersContainingOffset(
            state.chapters,
            locator.href,
            locator.textOffset,
            doc.doc
        )
        if (!candidates.length) return null
        if (candidates.length > 1) {
            const chapter = anchorChapter()
            if (candidates.includes(chapter)) return chapter
        }
        return candidates[0]!
    }

    /** The backend's saved passage applies until something is on screen. */
    async function applyEntry(current: BookEntry, mine: Navigation) {
        if (!(await booted)) return finish(mine)
        if (!isCurrent(mine)) return
        const initial = !mounted.length
        const source = structure()

        if (current.ch) {
            const index = spineIndexOf(source, current.ch)
            if (index === -1 || !source.spine[index]!.linear) {
                if (await openStandalone(current.ch, current.frag ?? '', mine)) return
                if (!isCurrent(mine)) return
                if (!initial) return finish(mine)
            }
        }

        const urlPosition = current.ch ? await positionFor(current.ch, current.frag ?? '') : null
        if (!isCurrent(mine)) return
        const urlChapter = urlPosition ? chapterIndexForPosition(state.chapters, urlPosition) : null

        const locator = nav.historyLocator() ?? (initial ? savedLocator() : null)
        const locatorChapter = locator ? await chapterForLocator(locator) : null
        if (!isCurrent(mine)) return

        const choice = chooseEntryChapter(urlChapter, locatorChapter)
        const anchor =
            !choice.useLocator && current.ch && current.frag
                ? { href: current.ch, fragment: current.frag }
                : null
        const landing = mine.landing ?? {
            locator: choice.useLocator ? locator : urlPosition && positionLocator(urlPosition),
            anchor,
        }
        const landed = await navigateTo(choice.chapterIndex, landing, mine)
        if (!isCurrent(mine)) return
        // After a failure, back onto the chapter still shown, so a retry is a
        // new route rather than a duplicate of this one.
        canonicalize(landed ? anchor : null)
        finish(mine)
    }

    function canonicalize(anchor: BookAnchor | null) {
        const chapter = state.chapters[state.chapterIndex]
        if (!chapter || state.standalone) return
        const target = anchor ?? chapter.target
        currentEntryKey = entryKey({ ch: target.href, frag: target.fragment || null })
        nav.replace(target)
    }

    /** Guarded at the point of entry as well as on disposal: the queue can
     * still be appended to after `dispose` has swept it. */
    function waitForHost(): Promise<void> {
        if (host || disposed()) return Promise.resolve()
        return new Promise(resolve => {
            hostWaiters.push(resolve)
        })
    }

    async function buildSlices(
        slices: ChapterSlice[],
        mine: Navigation
    ): Promise<MountedSlice[] | null> {
        mine.phase = 'loading'
        const documents = await Promise.all(slices.map(slice => prepare(slice.href)))
        if (!isCurrent(mine)) return null
        await waitForHost()
        if (!isCurrent(mine) || !host) return null

        return slices.map((slice, i) => {
            const source = documents[i]!
            const body = docBody(source.doc)!.cloneNode(true) as HTMLElement
            pruneToRange(body, slice.start, slice.end)
            const holder = document.createElement('div')
            holder.addEventListener('click', onClick)
            // Paged mode hides it: an empty multicol still takes a column.
            holder.classList.toggle('is-empty', isEmptySlice(body, source.styles))
            holder.classList.toggle('is-paged', layout.value.kind === 'paged')
            return {
                slice,
                prepared: source,
                holder,
                root: mountTree(holder, source, body, settings().fontFamily === 'publisher'),
                startOffset: textOffsetAtPath(docBody(source.doc)!, slice.start),
            }
        })
    }

    /** Vertical writing isn't paginated: such a chapter scrolls instead. */
    function modeFor(preferred: LayoutKind): LayoutKind {
        if (preferred === 'scroll') return preferred
        const vertical = mounted.some(slice =>
            [slice.root, slice.root.parentElement!].some(
                el => getComputedStyle(el).writingMode !== 'horizontal-tb'
            )
        )
        return vertical ? 'scroll' : preferred
    }

    /** A passage that isn't mounted lands at the start instead. */
    function place(landing: Landing | null, provisional = false) {
        const placed = layout.value.place(landing, provisional)
        if (placed === false) layout.value.place('start', provisional)
        return placed
    }

    /** Where the destination becomes observable. Nothing before it writes
     * reader state, so a superseded navigation leaves none of it half-moved.
     * Paged paints the right screen first; scroll mode waits at an end, not
     * scrolling into a chapter whose images are still loading. */
    async function land(
        next: MountedSlice[],
        destination: number | Standalone,
        landing: Landing,
        mine: Navigation
    ) {
        host!.replaceChildren(...next.map(slice => slice.holder))
        mounted = next
        state.firstChapterMounted = true
        inputSinceChapter = false
        lastStamp = 0
        state.error = null
        // Other notices can be about this very destination (a missing fragment).
        if (state.notice === LOAD_FAILED) state.notice = null
        if (typeof destination === 'number') state.chapterIndex = destination
        // Raw: its locator goes into `history.state`, which can't clone a proxy.
        state.standalone = typeof destination === 'number' ? null : markRaw(destination)
        mine.phase = 'settling'
        mine.landing = landing

        const mode = modeFor(settings().mode)
        if (mode === layout.value.kind) layout.value.attach(host, mounted)
        else useLayout(mode, null)
        place(landing, true)
        await settle()
        if (!isCurrent(mine)) return
        const target = mine.landing
        if (place(target) === false && typeof target === 'object' && target?.anchor) {
            state.notice = 'That link points somewhere unavailable.'
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

    /** Resolves whether the chapter was put on screen. */
    async function navigateTo(index: number, landing: Landing, mine: Navigation) {
        const chapter = state.chapters[index]
        if (!chapter) return false
        const hadMounted = mounted.length > 0
        let next: MountedSlice[] | null = null
        try {
            next = await buildSlices(chapter.slices, mine)
        } catch (err) {
            if (isCurrent(mine)) {
                console.error(err)
                // A reader already on a chapter keeps it; only a failed first
                // load leaves nothing to show.
                if (hadMounted) state.notice = LOAD_FAILED
                else state.error = err instanceof Error ? err.message : String(err)
            }
        }
        if (!next || !isCurrent(mine)) return false
        await land(next, index, landing, mine)
        return true
    }

    /** Outside the flow, keeping the passage it was opened from for the way
     * back. True once shown. */
    async function openStandalone(href: string, fragment: string, mine: Navigation) {
        const returnTo = state.standalone?.returnTo ?? {
            chapterIndex: state.chapterIndex,
            locator: captureLocator() ?? pendingLocator,
        }
        if (!state.standalone) snapshotPassage()
        const source = structure()
        const index = spineIndexOf(source, href)
        const next = await buildSlices(
            [{ href, spineIndex: index, start: null, end: null }],
            mine
        ).catch(() => null)
        if (!isCurrent(mine)) return false
        if (!next) {
            state.notice = 'That part of the book is unavailable.'
            return false
        }
        const title = source.spine[index]?.title || href
        const landing = fragment ? { anchor: { href, fragment } } : 'start'
        await land(next, { href, title, returnTo }, landing, mine)
        finish(mine)
        return true
    }

    function captureLocator(): BookLocator | null {
        return state.standalone ? null : layout.value.capture()
    }

    function clearStampTimer() {
        if (stampTimer) clearTimeout(stampTimer)
        stampTimer = null
    }

    /** Rate-limited so scrolling can't flood `replaceState`, but with a
     * trailing stamp so the last position is never the one left out. */
    function stampLocator(locator: BookLocator, force = false) {
        if (disposed()) return
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

    /** Only while the entry still shows the mounted chapter: once a route
     * change has begun, the pending passage belongs to the entry left behind. */
    function stampNow() {
        if (!canCapture()) return
        captureNow()
        if (pendingLocator) stampLocator(pendingLocator, true)
    }

    function snapshotPassage() {
        stampNow()
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
        if (!canCapture() || !state.structure) return
        const locator = layout.value.capture()
        if (locator) setLocator(locator, stamp)
    }

    const captureSoon = useDebounceFn(captureNow, CAPTURE_DEBOUNCE)

    function nextStatus(): ReadingStatus | undefined {
        if (userData?.status && userData.status !== 'reading') return undefined
        if (completed) return 'completed'
        return navigated ? 'reading' : undefined
    }

    function write(final = false) {
        if (!state.content || !pendingLocator || closing) return
        const locator = pendingLocator
        const status = nextStatus()
        const statusChanged = !!status && status !== userData?.status
        const parentId = state.content.parent_id
        const payload = {
            status,
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
                .then(() => {
                    void (statusChanged
                        ? invalidateStatusChange(parentId)
                        : invalidateRecentlyRead())
                })
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
                if (statusChanged) void invalidateStatusChange(parentId)
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
        if (completed || state.standalone || !inputSinceChapter || !sentinel) return
        if (state.chapterIndex !== state.chapters.length - 1) return
        const rect = sentinel.getBoundingClientRect()
        if (rect.bottom <= 0 || rect.top >= window.innerHeight) return
        completed = true
        void persist()
    }

    function onUserMove() {
        navigated = true
        const current = activity.value
        if (current.phase === 'settling') current.landing = null
        if (!canCapture()) return
        void captureSoon()
        checkCompletion()
    }

    function onInput() {
        // The input that dismisses a modal isn't reading: without this, it can
        // mark a short final chapter completed.
        if (restoring.value || hasOpenModal.value) return
        inputSinceChapter = true
        checkCompletion()
    }

    function onVisibility() {
        if (document.visibilityState === 'visible') {
            closing = false
            return
        }
        flushProgress(true)
    }

    /** Restores the passage after a layout change of our own. A navigation
     * places the reader itself once it settles. */
    function reflow() {
        if (!restoring.value) layout.value.reflow()
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
     * another chapter than the one the link sits on. */
    async function followLink(href: string, fragment: string) {
        navigated = true
        const mine = start('resolving')
        const source = structure()
        const index = spineIndexOf(source, href)

        if (index === -1 || !source.spine[index]!.linear) {
            if (!(await openStandalone(href, fragment, mine))) finish(mine)
            return
        }

        const doc = await preparedFor(href)
        if (!isCurrent(mine)) return
        if (!doc) {
            state.notice = 'That part of the book is missing.'
            return finish(mine)
        }
        const found = fragment ? !!resolveTargetElement(doc.doc, fragment) : false
        state.notice = fragment && !found ? 'That link points somewhere unavailable.' : null

        if (
            found &&
            !state.standalone &&
            layout.value.place({ anchor: { href, fragment } }) === true
        ) {
            return finish(mine)
        }

        const position = await positionFor(href, found ? fragment : '')
        if (!isCurrent(mine)) return
        const chapter = position ? chapterIndexForPosition(state.chapters, position) : -1
        if (!position || chapter === -1) return finish(mine)

        const anchor = found ? { href, fragment } : null
        const destination = anchor ?? { href, fragment: '' }
        // A link naming the entry we are already on gets no route change, so
        // this navigation has to finish the transition itself.
        if (
            entryKey({ ch: destination.href, frag: destination.fragment || null }) !==
            currentEntryKey
        ) {
            return push(destination, mine)
        }
        const landing = { anchor, locator: anchor ? null : positionLocator(position) }
        if (chapter === state.chapterIndex && mounted.length && !state.standalone) {
            layout.value.place(landing)
        } else {
            await navigateTo(chapter, landing, mine)
        }
        finish(mine)
    }

    /** Through the router, whose watcher calls `setEntry`: one history entry
     * per destination. */
    function push(target: BookAnchor, mine: Navigation) {
        snapshotPassage()
        mine.phase = 'routing'
        mine.entry = entryKey({ ch: target.href, frag: target.fragment || null })
        void nav.push(target).then(
            failure => {
                if (isNavigationFailure(failure)) finish(mine)
            },
            () => finish(mine)
        )
    }

    function onPageHide() {
        flushProgress(true)
    }

    function goToChapter(index: number, atEnd = false) {
        const target = state.chapters[index]?.target
        if (!target || crossing()) return
        push(target, start('routing', atEnd ? 'end' : null))
    }

    async function closeStandalone() {
        const back = state.standalone?.returnTo
        const index = back?.chapterIndex ?? state.chapterIndex
        const mine = start('loading')
        await navigateTo(index, { locator: back?.locator }, mine)
        if (!isCurrent(mine)) return
        canonicalize(null)
        finish(mine)
    }

    function endBook() {
        // Supersedes a navigation still settling, which would canonicalize.
        if (activity.value.phase === 'settling') canonicalize(null)
        activity.value = { phase: 'book-end' }
        if (completed) return
        completed = true
        void persist()
    }

    /** Paged: one screen on, crossing chapters at either end. The end of the
     * book is one more screen, and turning onto it completes the book. */
    function turn(direction: 'next' | 'prev') {
        if (crossing() || !mounted.length) return
        if (activity.value.phase === 'book-end') {
            if (direction === 'prev') activity.value = { phase: 'ready' }
            return
        }
        const result = layout.value.turn(direction)
        if (result === 'unavailable') return
        if (result === 'moved') return onUserMove()
        if (state.standalone) void closeStandalone()
        else if (result === 'start') {
            if (state.chapterIndex > 0) goToChapter(state.chapterIndex - 1, true)
        } else if (state.chapterIndex < state.chapters.length - 1) {
            goToChapter(state.chapterIndex + 1)
        } else {
            endBook()
        }
    }

    /** Paged: jumps within the chapter, clamped. */
    function goToScreen(index: number) {
        if (crossing() || !mounted.length) return
        if (activity.value.phase === 'book-end') activity.value = { phase: 'ready' }
        if (layout.value.showScreen(index)) onUserMove()
    }

    function setMode(preferred: LayoutKind) {
        if (activity.value.phase === 'book-end') activity.value = { phase: 'ready' }
        const mode = modeFor(preferred)
        if (mode === layout.value.kind) return
        useLayout(mode, layout.value.capture())
        const current = activity.value
        if (current.phase === 'settling' && current.landing) {
            layout.value.place(current.landing, true)
        }
    }

    /** The new layout opens at `seed`, and binds once Vue has rendered its CSS. */
    function useLayout(mode: LayoutKind, seed: BookLocator | null) {
        layout.value.dispose()
        const next = createLayout(mode, seed)
        layout.value = next
        for (const slice of mounted) slice.holder.classList.toggle('is-paged', mode === 'paged')
        void nextTick(() => {
            if (layout.value === next) next.attach(host, mounted)
        })
    }

    function createLayout(mode: LayoutKind, seed: BookLocator | null) {
        return createReadingLayout(mode, { seed, spread: () => settings().spread, onUserMove })
    }

    const inputEvents = ['pointerdown', 'keydown', 'wheel', 'touchstart'] as const
    const layout = shallowRef<ReadingLayout>(createLayout(settings().mode, null))
    /** `pre`: the passage is captured against the old layout, before the DOM
     * changes under it. The user stylesheet is rebuilt in place, since a
     * remount would lose the position. */
    const stopSettings = watch(
        () => ({ ...settings() }),
        (next, prev) => {
            if (next.mode !== prev.mode) setMode(next.mode)
            else reflow()
            const publisher = next.fontFamily === 'publisher'
            if (publisher === (prev.fontFamily === 'publisher')) return
            for (const slice of mounted) updateUserStyles(slice.holder, publisher)
        },
        { flush: 'pre' }
    )
    // The rate-limited stamp can lag a turn by half a second.
    const removeLeaveGuard = nav.beforeLeave(stampNow)
    window.addEventListener('pagehide', onPageHide)
    document.addEventListener('visibilitychange', onVisibility)
    for (const name of inputEvents) window.addEventListener(name, onInput, { passive: true })

    const chapter = computed(() => state.chapters[state.chapterIndex] ?? null)

    const booted = init()
    void applyEntry(entry, activity.value as Navigation)

    return reactive({
        ...toRefs(state),
        chapter,
        nextChapter: computed(() => state.chapters[state.chapterIndex + 1] ?? null),
        prevChapter: computed(() => state.chapters[state.chapterIndex - 1] ?? null),
        title: computed(() => state.standalone?.title ?? chapter.value?.title ?? ''),
        restoring,
        /** The layout in effect, which a chapter can override (vertical writing). */
        layoutMode: computed(() => layout.value.kind),
        /** Paged: the screen shown and the screens in the chapter. */
        screen: computed(() => layout.value.screen),
        /** Paged: past the last screen of the book. */
        atBookEnd: computed(() => activity.value.phase === 'book-end'),

        setElements(elements: { host: HTMLElement | null; sentinel: HTMLElement | null }) {
            host = elements.host
            sentinel = elements.sentinel
            if (host && mounted.some(slice => slice.holder.parentNode !== host)) {
                host.replaceChildren(...mounted.map(slice => slice.holder))
            }
            layout.value.attach(host, mounted)
            if (host) while (hostWaiters.length) hostWaiters.pop()!()
        },

        setEntry(next: BookEntry) {
            const key = entryKey(next)
            // A restore's canonicalized entry comes back here with the same key.
            if (key === currentEntryKey) return
            navigated = true
            currentEntryKey = key
            // The history entry has already moved, so this passage belongs to
            // the one we are leaving and must not be stamped onto it.
            flushProgress(false, false)
            clearStampTimer()
            const current = activity.value
            // A push of our own arrives here; it keeps its landing.
            const mine =
                current.phase === 'routing' && current.entry === key ? current : start('loading')
            mine.phase = 'loading'
            mine.entry = key
            void applyEntry(next, mine)
        },

        snapshotPassage,
        goToChapter,
        closeStandalone,
        turn,
        goToScreen,

        dismissNotice() {
            state.notice = null
        },

        dispose() {
            if (disposed()) return writeChain
            clearStampTimer()
            flushProgress(true, false)
            activity.value = { phase: 'disposed' }
            stopSettings()
            controller.abort()
            window.removeEventListener('pagehide', onPageHide)
            removeLeaveGuard()
            document.removeEventListener('visibilitychange', onVisibility)
            for (const name of inputEvents) window.removeEventListener(name, onInput)
            while (hostWaiters.length) hostWaiters.pop()!()
            captureSoon.cancel()
            prefetch.cancel()
            layout.value.dispose()
            return writeChain
        },
    })
}

export type BookSession = ReturnType<typeof createBookSession>
