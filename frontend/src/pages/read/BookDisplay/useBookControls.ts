import { onMounted, onUnmounted } from 'vue'
import { keysOwnedElsewhere, navDrawerOpen } from '@/ui/overlay'
import { getLayoutTop } from '@/utils/misc'
import { hasOpenModal } from '@/utils/modals'
import { isDrawerToggle } from '../shortcuts'
import { getClickZone } from '../useClickZones'
import { hasAttr, shadowSelection } from './domSafe'
import { createSwipe } from './swipe'
import { useBookDisplayStore } from './useBookDisplayStore'
import { createWheelTurns } from './wheelTurns'

const PAGE_FACTOR = 0.85
const EDGE_MARGIN = 20
const DRAG_SLOP = 8

/** The window's selection, else that of the tree the event came from. */
function selectedText(event: Event): string {
    const text = window.getSelection()?.toString() ?? ''
    if (text) return text
    const root = event.composedPath().find(node => node instanceof ShadowRoot)
    return root ? (shadowSelection(root as ShadowRoot)?.toString() ?? '') : ''
}

export function isZoomed() {
    return (window.visualViewport?.scale ?? 1) > 1
}

/** A tap is only a tap when the click means nothing else. */
export function createTapGuard() {
    let start: { x: number; y: number } | null = null
    let startedSelected = false

    return {
        down(event: PointerEvent) {
            start = { x: event.clientX, y: event.clientY }
            // The press collapses the selection before the click fires, so a
            // click that only dismisses one has to be judged from here.
            startedSelected = !!selectedText(event)
        },
        allows(event: MouseEvent): boolean {
            const from = start
            const dismissed = startedSelected
            start = null
            startedSelected = false
            if (event.defaultPrevented || event.button !== 0) return false
            if (from && Math.hypot(event.clientX - from.x, event.clientY - from.y) > DRAG_SLOP) {
                return false
            }
            if (dismissed || selectedText(event)) return false
            // Links keep the click: in-book ones through the session's own
            // handler, external ones through the browser.
            return !event
                .composedPath()
                .some(
                    node =>
                        node instanceof Element &&
                        (hasAttr(node, 'href') || hasAttr(node, 'data-book-missing'))
                )
        },
    }
}

export function useBookControls() {
    const store = useBookDisplayStore()
    const guard = createTapGuard()
    let clickMoved = false
    const wheelTurns = createWheelTurns()
    const swipe = createSwipe<TouchEvent>({
        hasSelection: e => !!selectedText(e),
        isZoomed,
    })

    /** Wheel and swipe turn pages only over the pages themselves. */
    function pagedSession() {
        const session = store.session
        if (session?.layoutMode !== 'paged' || store.sidebarOpen || hasOpenModal.value) return null
        return session
    }

    function atTop() {
        return window.scrollY <= EDGE_MARGIN
    }

    function atBottom() {
        return (
            window.scrollY + window.innerHeight >=
            document.documentElement.scrollHeight - EDGE_MARGIN
        )
    }

    function handleMove(direction: 'next' | 'prev') {
        const session = store.session
        if (!session) return
        if (session.layoutMode === 'paged') {
            session.turn(direction)
            return
        }
        if (direction === 'next' ? atBottom() : atTop()) {
            // Backwards out of a chapter lands at the bottom of the one before,
            // so paging back doesn't skip it.
            goToChapter(direction === 'next' ? 1 : -1, direction === 'prev')
            return
        }
        const step = (window.innerHeight - getLayoutTop()) * PAGE_FACTOR
        window.scrollBy({ top: direction === 'next' ? step : -step, behavior: 'smooth' })
    }

    function goToChapter(delta: number, atEnd = false) {
        const session = store.session
        if (!session) return
        if (session.standalone) {
            void session.closeStandalone()
            return
        }
        session.goToChapter(session.chapterIndex + delta, atEnd)
    }

    function handleKeydown(e: KeyboardEvent) {
        if (keysOwnedElsewhere(e) || e.metaKey || e.ctrlKey || e.altKey) return
        if (isDrawerToggle(e)) {
            // Under an open main-nav drawer it would open unseen.
            if (!navDrawerOpen.value) store.sidebarOpen = !store.sidebarOpen
            e.preventDefault()
            return
        }
        // No page turns while the drawer is open (ADrawer handles Esc).
        if (store.sidebarOpen) return

        // Space activates whatever chrome has focus; the arrows mean nothing
        // to a button, so they keep paging.
        if (e.key === ' ' && document.activeElement instanceof HTMLButtonElement) return

        switch (e.key) {
            case 'ArrowDown':
            case 'ArrowRight':
            case 'PageDown':
                handleMove('next')
                break
            case 'ArrowUp':
            case 'ArrowLeft':
            case 'PageUp':
                handleMove('prev')
                break
            case ' ':
                handleMove(e.shiftKey ? 'prev' : 'next')
                break
            case ',':
                goToChapter(-1)
                break
            case '.':
                goToChapter(1)
                break
            default:
                return
        }
        e.preventDefault()
    }

    onMounted(() => {
        window.addEventListener('keydown', handleKeydown)
    })

    onUnmounted(() => {
        window.removeEventListener('keydown', handleKeydown)
    })

    return {
        handlePointerDown: guard.down,
        /** A quick click after a turn is another turn, not a double-click that
         * selects the word now under the pointer, which the guard would refuse. */
        handleMouseDown(e: MouseEvent) {
            if (e.detail > 1 && clickMoved) e.preventDefault()
        },
        handleClick(e: MouseEvent) {
            clickMoved = false
            if (!guard.allows(e)) return
            const zone = getClickZone(e)
            if (zone === 'menu') store.sidebarOpen = true
            // Zoomed in, a tap is more likely panning than turning.
            else if (!isZoomed() || store.session?.layoutMode !== 'paged') {
                handleMove(zone)
                clickMoved = true
            }
        },
        handleWheel(e: WheelEvent) {
            const session = pagedSession()
            const direction = session && wheelTurns(e)
            if (direction) session.turn(direction)
        },
        handleTouchStart(e: TouchEvent) {
            if (pagedSession()) swipe.start(e)
        },
        handleTouchEnd(e: TouchEvent) {
            const session = pagedSession()
            const direction = session && swipe.end(e, window.innerWidth)
            if (direction) session.turn(direction)
        },
    }
}
