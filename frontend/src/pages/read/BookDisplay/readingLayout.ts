import { useDebounceFn } from '@vueuse/core'
import { reactive } from 'vue'
import type { BookLocator } from '@/utils/api/types'
import { getLayoutTop } from '@/utils/misc'
import type { BookSpread } from './bookSettings'
import {
    captureIn,
    locateAnchorIn,
    locateIn,
    locatorAt,
    SCROLL_MARGIN,
    scrollFrame,
    type LocatableSlice,
} from './locatorGeometry'
import { createPaginator, type Paginator, type ScreenState } from './paginator'

export type LayoutKind = 'scroll' | 'paged'

/** Where to put the reader: a chapter end, or a passage (the authored anchor
 * if it's there, else the locator). */
export type Landing =
    | 'start'
    | 'end'
    | { anchor?: { href: string; fragment: string } | null; locator?: BookLocator | null }

/** How the mounted chapter is laid out and moved through: the window scrolls
 * in scroll mode, the frame turns screens in paged mode. Each layout owns the
 * passage it returns to after a layout change (its anchor). */
export interface ReadingLayout {
    readonly kind: LayoutKind
    /** Paged: the screen shown and the screens in the chapter. */
    readonly screen: ScreenState | null
    /** Binds to the host (null: unbinds) and the mounted slices. Binding a new
     * host lands where `place` was asked to before, or back at the anchor. */
    attach(host: HTMLElement | null, slices: LocatableSlice[]): void
    /** The passage in view. */
    capture(): BookLocator | null
    /** Lands on `landing`, or re-measures and returns to the anchor when null.
     * False, without moving, when the passage isn't mounted. Before `attach`,
     * it's kept for then ('pending'). Provisional: scroll mode only goes to an
     * end, so a chapter still loading images isn't scrolled into. */
    place(landing: Landing | null, provisional?: boolean): Placed
    turn(direction: 'next' | 'prev'): 'moved' | 'start' | 'end' | 'unavailable'
    /** Paged: shows the screen at `index`, clamped. False without a paginator. */
    showScreen(index: number): boolean
    /** The text layout changed: lays out again, keeping the passage. */
    reflow(): void
    dispose(): void
}

export interface LayoutOptions {
    /** The passage to open at, for a layout replacing another. */
    seed: BookLocator | null
    spread(): BookSpread
    /** A move the layout didn't make itself. */
    onUserMove(): void
}

export type Placed = boolean | 'pending'

interface Pending {
    landing: Landing | null
    provisional: boolean
}

const RESIZE_DEBOUNCE = 100
const REFLOW_DEBOUNCE = 200

function locateLanding(slices: LocatableSlice[], landing: Exclude<Landing, string>) {
    return (
        (landing.anchor ? locateAnchorIn(slices, landing.anchor) : null) ??
        (landing.locator ? locateIn(slices, landing.locator) : null)
    )
}

function locatorOf(pending: Pending | null) {
    const landing = pending?.landing
    return typeof landing === 'object' ? (landing?.locator ?? null) : null
}

/** Before `attach`, the last landing asked for wins over earlier ones. */
function keep(pending: Pending | null, landing: Landing | null, provisional: boolean): Pending {
    return landing || !pending ? { landing, provisional } : pending
}

/** Scroll mode keeps the locator it restored until the reader scrolls:
 * recapturing would take the start of the line the passage now sits mid-way
 * along, drifting back a line per layout change. */
export function createScrollLayout(options: LayoutOptions): ReadingLayout {
    let slices: LocatableSlice[] = []
    let attached = false
    let anchor = options.seed
    let pending: Pending | null = null
    let programmaticScrolls = 0
    /** Scrolls from native anchoring during a reflow aren't the reader's. */
    let reflowing = false
    let disposed = false

    function scrollWindowTo(top: number) {
        if (disposed) return
        programmaticScrolls++
        window.scrollTo({ top: Math.max(0, top), behavior: 'instant' })
        requestAnimationFrame(() => {
            programmaticScrolls = Math.max(0, programmaticScrolls - 1)
        })
    }

    function reveal(target: Element | Range) {
        scrollWindowTo(
            window.scrollY + target.getBoundingClientRect().top - getLayoutTop() - SCROLL_MARGIN
        )
    }

    function capture() {
        if (!attached) return locatorOf(pending) ?? anchor
        return anchor ?? captureIn(slices, scrollFrame(getLayoutTop(), window.innerHeight))
    }

    function place(landing: Landing | null, provisional = false): Placed {
        if (!attached) {
            pending = keep(pending, landing, provisional)
            return 'pending'
        }
        if (!landing) {
            const target = anchor && locateIn(slices, anchor)
            if (target) reveal(target)
            return true
        }
        if (landing === 'start' || landing === 'end' || provisional) {
            anchor = null
            scrollWindowTo(landing === 'end' ? document.documentElement.scrollHeight : 0)
            return true
        }
        const target = locateLanding(slices, landing)
        if (!target) return false
        reveal(target)
        anchor = null
        return true
    }

    const reflowEnd = useDebounceFn(() => {
        reflowing = false
        const target = !disposed && anchor && locateIn(slices, anchor)
        if (target) reveal(target)
    }, REFLOW_DEBOUNCE)

    function onScroll() {
        if (programmaticScrolls || reflowing || !attached) return
        anchor = null
        options.onUserMove()
    }

    window.addEventListener('scroll', onScroll, { passive: true })

    return {
        kind: 'scroll',
        screen: null,
        attach(host, next) {
            const binding = !attached && !!host
            slices = next
            attached = !!host
            if (!binding) return
            const kept = pending
            pending = null
            place(kept?.landing ?? null, kept?.provisional)
        },
        capture,
        place,
        turn: () => 'unavailable',
        showScreen: () => false,
        /** Window resizes are left to native scroll anchoring: a height-only
         * one (the mobile address bar) doesn't reflow, and restoring on it
         * would jerk the text. */
        reflow() {
            if (!attached) return
            reflowing = true
            anchor ??= capture()
            void reflowEnd()
        },
        dispose() {
            disposed = true
            reflowEnd.cancel()
            window.removeEventListener('scroll', onScroll)
        },
    }
}

/** Screens of columns. The anchor follows every screen change, and every
 * relayout returns to it. Relayouts are coalesced to one per frame, which also
 * throttles them on chapters that take long to lay out. */
export function createPagedLayout(options: LayoutOptions): ReadingLayout {
    const screen = reactive<ScreenState>({ index: 0, count: 1, spread: 1 })
    let paginator: Paginator | null = null
    let observer: ResizeObserver | null = null
    let slices: LocatableSlice[] = []
    let anchor = options.seed
    let pending: Pending | null = null
    let resizeTimer: ReturnType<typeof setTimeout> | null = null
    let frame: number | null = null
    let disposed = false
    const watchedRoots = new WeakSet<Node>()
    /** Slices outlive the layout (a mode switch keeps them mounted), so their
     * listeners must go with it. */
    const listeners = new AbortController()
    const fonts = (document as Document & { fonts?: FontFaceSet }).fonts

    function bind(host: HTMLElement | null) {
        paginator?.dispose()
        observer?.disconnect()
        paginator = null
        observer = null
        if (!host?.parentElement) return
        paginator = createPaginator(host, screen, {
            spread: options.spread,
            onUserMove() {
                anchor = captureIn(slices, paginator!.frame())
                options.onUserMove()
            },
        })
        let first = true
        observer = new ResizeObserver(() => {
            // Observing reports the current size once, which is no change.
            if (first) return void (first = false)
            if (resizeTimer) clearTimeout(resizeTimer)
            resizeTimer = setTimeout(schedule, RESIZE_DEBOUNCE)
        })
        // Border box: the paginator's own top padding would count as a resize.
        observer.observe(host.parentElement, { box: 'border-box' })
    }

    function show(index: number) {
        paginator!.showScreen(index)
        anchor = captureIn(slices, paginator!.frame())
    }

    function place(landing: Landing | null, provisional = false): Placed {
        if (disposed) return false
        if (!paginator) {
            pending = keep(pending, landing, provisional)
            return 'pending'
        }
        // Also picks up text settings changed while a navigation settled.
        paginator.layout()
        paginator.measure()
        if (landing === 'start' || landing === 'end') {
            show(landing === 'start' ? 0 : screen.count - 1)
            return true
        }
        const target = landing ? locateLanding(slices, landing) : anchor && locateIn(slices, anchor)
        if (landing && !target) return false
        paginator.showScreen((target && paginator.screenOf(target)) ?? screen.index)
        if (landing) anchor = (!landing.anchor && landing.locator) || locatorAt(slices, target!)
        return true
    }

    function schedule() {
        frame ??= requestAnimationFrame(() => {
            frame = null
            place(null)
        })
    }

    fonts?.addEventListener('loadingdone', schedule, { signal: listeners.signal })

    return {
        kind: 'paged',
        screen,
        attach(host, next) {
            if (disposed) return
            const rebind = host !== (paginator?.host ?? null)
            slices = next
            // Late images change the column count.
            for (const slice of next) {
                const root = slice.root.getRootNode()
                if (watchedRoots.has(root)) continue
                watchedRoots.add(root)
                root.addEventListener('load', schedule, { capture: true, signal: listeners.signal })
            }
            if (!rebind) return
            bind(host)
            if (!paginator) return
            // Whatever scroll mode left on the window would offset every rect.
            if (window.scrollY) window.scrollTo({ top: 0, behavior: 'instant' })
            const kept = pending
            pending = null
            place(kept?.landing ?? null)
        },
        capture: () =>
            paginator
                ? (anchor ?? captureIn(slices, paginator.frame()))
                : (locatorOf(pending) ?? anchor),
        place,
        turn(direction) {
            if (!paginator) return 'unavailable'
            // Counts can be stale (a late image, a font): re-read them before
            // leaving, so no text is skipped.
            if (direction === 'next' && screen.index >= screen.count - 1) place(null)
            const index = screen.index + (direction === 'next' ? 1 : -1)
            if (index < 0) return 'start'
            if (index >= screen.count) return 'end'
            show(index)
            return 'moved'
        },
        showScreen(index) {
            if (!paginator) return false
            show(index)
            return true
        },
        reflow: schedule,
        dispose() {
            disposed = true
            listeners.abort()
            bind(null)
            if (resizeTimer) clearTimeout(resizeTimer)
            if (frame != null) cancelAnimationFrame(frame)
        },
    }
}

export function createReadingLayout(kind: LayoutKind, options: LayoutOptions): ReadingLayout {
    return kind === 'scroll' ? createScrollLayout(options) : createPagedLayout(options)
}
