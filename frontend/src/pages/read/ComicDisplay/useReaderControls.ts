import { onMounted, onUnmounted } from 'vue'
import { getScrollParent, getViewportHeight } from '@/utils/css'
import { getLayoutTop } from '@/utils/misc'
import { getClickZone, shouldIgnoreKeys } from '../useClickZones'
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
        let switchToSibling = false
        const mode = reader.mode
        if (mode === 'longstrip') {
            if (direction === 'next') {
                if (isAtBottom()) {
                    switchToSibling = true
                } else {
                    scrollByViewport(0.85)
                }
            } else {
                if (isAtTop()) {
                    switchToSibling = true
                } else {
                    scrollByViewport(-0.85)
                }
            }
        } else {
            // Paged mode
            const currentPage = reader.state?.page ?? 0
            const pages = reader.state?.pageDimensions ?? []

            const newPage = direction === 'next' ? currentPage + 1 : currentPage - 1
            if (newPage >= 0 && newPage < pages.length) {
                reader.setPage(newPage)
            } else {
                switchToSibling = true
            }
        }

        if (switchToSibling) {
            // This makes sure that, on .dispose(), we correctly mark the
            // chapter as completed. Since progress in longstrip mode is based
            // on which image is in the middle of the viewport, we may be at the
            // end even though it hasn't set the page to the last one.
            if (direction == 'next' && reader.mode === 'longstrip' && reader.state) {
                reader.setPage(reader.state?.pageDimensions.length - 1)
            }
            reader.goToSibling(direction, direction === 'prev')
        }
    }

    function handleKeydown(e: KeyboardEvent) {
        if (shouldIgnoreKeys()) return

        switch (e.key) {
            case 'ArrowLeft':
                handleMove('prev')
                break
            case 'ArrowRight':
                handleMove('next')
                break
            case ',':
                reader.goToSibling('prev', true)
                break
            case '.':
                reader.goToSibling('next', false)
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
