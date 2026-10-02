import { onMounted, onUnmounted } from 'vue'
import { keysOwnedElsewhere, navDrawerOpen } from '@/ui/overlay'
import { getScrollParent, getViewportHeight } from '@/utils/css'
import { getLayoutTop } from '@/utils/misc'
import { isDrawerToggle } from '../shortcuts'
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

        if (reader.atEnd) {
            if (direction === 'prev') reader.atEnd = false
            // The next sibling may have arrived since.
            else reader.goPastEnd()
            return
        }
        const newPage = state.page + (direction === 'next' ? 1 : -1)
        if (newPage >= 0 && newPage < state.pageDimensions.length) {
            reader.setPage(newPage)
        } else if (direction === 'prev') {
            reader.goToSibling('prev')
        } else if (state.pageDimensions.length) {
            // Past the last page: a deliberate finish.
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

        switch (e.key) {
            case 'ArrowLeft':
                handleMove('prev')
                break
            case 'ArrowRight':
                handleMove('next')
                break
            case ',':
                reader.goToSibling('prev')
                break
            case '.':
                reader.goToSibling('next')
                break
        }
    }

    onMounted(() => {
        window.addEventListener('keydown', handleKeydown)
    })

    onUnmounted(() => {
        window.removeEventListener('keydown', handleKeydown)
    })

    return {
        handleClick(e: MouseEvent) {
            const zone = getClickZone(e)
            if (zone === 'prev') {
                handleMove('prev')
            } else if (zone === 'next') {
                handleMove('next')
            } else {
                reader.sidebarOpen = true
            }
        },
    }
}
