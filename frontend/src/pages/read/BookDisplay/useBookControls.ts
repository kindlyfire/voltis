import { onMounted, onUnmounted } from 'vue'
import { keysOwnedElsewhere, navDrawerOpen } from '@/ui/overlay'
import { getLayoutTop } from '@/utils/misc'
import { isDrawerToggle } from '../shortcuts'
import { getClickZone } from '../useClickZones'
import { hasAttr } from './domSafe'
import { useBookDisplayStore } from './useBookDisplayStore'

const PAGE_FACTOR = 0.85
const EDGE_MARGIN = 20
const DRAG_SLOP = 8

/** Chrome keeps selections made inside a shadow root out of the window's own,
 * so the tree the click came from is asked as well. */
function selectedText(event: MouseEvent): string {
    const text = window.getSelection()?.toString() ?? ''
    if (text) return text
    const root = event
        .composedPath()
        .find((node): node is ShadowRoot => node instanceof ShadowRoot) as
        | (ShadowRoot & { getSelection?: () => Selection | null })
        | undefined
    return root?.getSelection?.()?.toString() ?? ''
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
        if (direction === 'next' ? atBottom() : atTop()) {
            // Backwards out of a page lands at the bottom of the one before,
            // so paging back doesn't skip it.
            goToPage(direction === 'next' ? 1 : -1, direction === 'prev')
            return
        }
        const step = (window.innerHeight - getLayoutTop()) * PAGE_FACTOR
        window.scrollBy({ top: direction === 'next' ? step : -step, behavior: 'smooth' })
    }

    function goToPage(delta: number, atEnd = false) {
        const session = store.session
        if (!session) return
        if (session.standalone) {
            void session.closeStandalone()
            return
        }
        session.goToPage(session.pageIndex + delta, atEnd)
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
                goToPage(-1)
                break
            case '.':
                goToPage(1)
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
        handleClick(e: MouseEvent) {
            if (!guard.allows(e)) return
            const zone = getClickZone(e)
            if (zone === 'menu') store.sidebarOpen = true
            else handleMove(zone)
        },
    }
}
