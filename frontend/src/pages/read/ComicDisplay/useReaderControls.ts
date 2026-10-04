import { onMounted, onUnmounted } from 'vue'
import { keysOwnedElsewhere, navDrawerOpen } from '@/ui/overlay'
import { getScrollParent, getViewportHeight } from '@/utils/css'
import { getLayoutTop } from '@/utils/misc'
import { hasOpenModal } from '@/utils/modals'
import { isDrawerToggle } from '../shortcuts'
import { createSwipe, isZoomed } from '../swipe'
import { getClickZone } from '../useClickZones'
import { useReaderStore } from './useComicDisplayStore'

const scrollParent = () => getScrollParent(document.getElementById('longstrip-container')!)

function isAtBottom(): boolean {
    const el = scrollParent()
    if (!el) return false
    return window.scrollY + getViewportHeight() > el.scrollHeight - 20
}

function isAtTop(): boolean {
    const el = scrollParent()
    if (!el) return true
    return el.scrollTop <= 10
}

function scrollByViewport(factor: number) {
    const el = scrollParent()
    if (!el) return
    el.scrollBy({ top: (el.clientHeight - getLayoutTop()) * factor, behavior: 'smooth' })
}

export function useReaderControls() {
    const reader = useReaderStore()

    function handleMove(direction: 'next' | 'prev') {
        const state = reader.state
        // Before the saved page is placed, input would overwrite it.
        if (!state || state.loading || state.error) return
        if (reader.mode === 'longstrip') {
            // Key and click scrolls are reading, so they arm the strip before it moves.
            reader.armed = true
            if (direction === 'next' ? !isAtBottom() : !isAtTop()) {
                scrollByViewport(direction === 'next' ? 0.85 : -0.85)
            } else if (direction === 'prev') {
                reader.goToSibling('prev')
            } else {
                state.finish()
                reader.goPastEnd()
            }
            return
        }

        // A page that doesn't fit is read through before turning.
        if (!reader.atEnd && reader.pagedScroller?.step(direction)) return
        turn(direction)
    }

    /** Paged: turns by spread, the one path for keys, clicks and swipes. */
    function turn(direction: 'next' | 'prev') {
        const state = reader.state
        if (!state || state.loading || state.error) return
        if (reader.atEnd) {
            if (direction === 'prev') reader.atEnd = false
            // The next sibling may have arrived since.
            else reader.goPastEnd()
            return
        }
        const index = reader.spreadIndex
        if (index === undefined) {
            // No pages.
            if (direction === 'prev') reader.goToSibling('prev')
            return
        }
        const target = reader.spreads[index + (direction === 'next' ? 1 : -1)]
        if (target) {
            const scroller = reader.pagedScroller
            if (scroller) scroller.enterAt = direction === 'next' ? 'start' : 'end'
            reader.setPage(target[0]!)
        } else if (direction === 'prev') {
            reader.goToSibling('prev')
        } else {
            // Past the last spread: a deliberate finish.
            state.finish()
            reader.goPastEnd()
        }
    }

    function handleKeydown(e: KeyboardEvent) {
        if (keysOwnedElsewhere(e)) return
        if (isDrawerToggle(e)) {
            // Under an open main-nav drawer it would open unseen.
            if (!navDrawerOpen.value) reader.sidebarOpen = !reader.sidebarOpen
            e.preventDefault()
            return
        }

        if (reader.mode === 'paged') {
            if (handlePagedKey(e)) e.preventDefault()
        } else if (e.key === 'ArrowLeft') {
            handleMove('prev')
        } else if (e.key === 'ArrowRight') {
            handleMove('next')
        }
        switch (e.key) {
            case ',':
                reader.goToSibling('prev')
                break
            case '.':
                reader.goToSibling('next')
                break
        }
    }

    /** True when the key was used. */
    function handlePagedKey(e: KeyboardEvent): boolean {
        if (e.ctrlKey || e.metaKey || e.altKey) return false
        const flipped = reader.controlsFlipped
        switch (e.key) {
            case 'ArrowLeft':
                handleMove(flipped ? 'next' : 'prev')
                return true
            case 'ArrowRight':
                handleMove(flipped ? 'prev' : 'next')
                return true
            case 'PageDown':
                handleMove('next')
                return true
            case 'PageUp':
                handleMove('prev')
                return true
            case ' ':
                // Space activates a focused button instead.
                if (document.activeElement instanceof HTMLButtonElement) return false
                handleMove(e.shiftKey ? 'prev' : 'next')
                return true
            case 'ArrowDown':
            case 'ArrowUp':
                reader.pagedScroller?.nudge(e.key === 'ArrowDown' ? 0.15 : -0.15)
                return true
        }
        if (e.key.toLowerCase() === 's' && !e.shiftKey && !e.repeat && reader.spreadDouble) {
            reader.placement()
            reader.toggleShift()
            return true
        }
        return false
    }

    const swipe = createSwipe<TouchEvent>({
        hasSelection: () => !!window.getSelection()?.toString(),
        isZoomed,
    })
    // Physical x edges the viewport was at when the touch began.
    let swipeEdges: { left: boolean; right: boolean } | null = null
    const swipeable = () => reader.mode === 'paged' && !reader.sidebarOpen && !hasOpenModal.value

    onMounted(() => {
        window.addEventListener('keydown', handleKeydown)
    })

    onUnmounted(() => {
        window.removeEventListener('keydown', handleKeydown)
    })

    return {
        handleClick(e: MouseEvent) {
            const zone = getClickZone(e, { flipped: reader.controlsFlipped })
            if (zone === 'prev') {
                handleMove('prev')
            } else if (zone === 'next') {
                handleMove('next')
            } else {
                reader.sidebarOpen = true
            }
        },
        handleTouchStart(e: TouchEvent) {
            if (!swipeable()) return
            swipe.start(e)
            const scroller = reader.atEnd ? null : reader.pagedScroller
            swipeEdges = scroller?.edges() ?? { left: true, right: true }
        },
        /** Turns only when the touch began at both the edge the finger reveals and the
         * reading-order edge for the move, so a pan reaching the edge doesn't also turn. In
         * non-inverted RTL those are opposite edges: an overflowing page only pans. */
        handleTouchEnd(e: TouchEvent) {
            if (!swipeable() || !swipeEdges) return
            const physical = swipe.end(e, window.innerWidth)
            if (!physical) return
            const revealed = physical === 'next' ? 'right' : 'left'
            const flip = { next: 'prev', prev: 'next' } as const
            const direction = reader.controlsFlipped ? flip[physical] : physical
            const readingEdge =
                (direction === 'next') !== (reader.direction === 'rtl') ? 'right' : 'left'
            if (swipeEdges[revealed] && swipeEdges[readingEdge]) turn(direction)
        },
    }
}
