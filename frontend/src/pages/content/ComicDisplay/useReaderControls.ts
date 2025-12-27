import { onMounted, onUnmounted } from 'vue'
import { useReaderStore } from './useComicDisplayStore'
import { getScrollParent, getViewportHeight } from '@/utils/css'
import { getLayoutTop } from '@/utils/misc'

type ClickZone = 'prev' | 'next' | 'menu'

const scrollParent = () => getScrollParent(document.getElementById('longstrip-container')!)

function isAtBottom(): boolean {
	const el = scrollParent()
	if (!el) return false
	return window.scrollY + getViewportHeight() > el.scrollHeight - 10
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
		const mode = reader.settings.mode
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
			reader.goToSibling(direction)
		}
	}

	function handleKeydown(e: KeyboardEvent) {
		// Ignore when an input element is focused
		const active = document.activeElement
		if (
			active instanceof HTMLInputElement ||
			active instanceof HTMLTextAreaElement ||
			active instanceof HTMLSelectElement ||
			(active instanceof HTMLElement && active.isContentEditable)
		) {
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
				reader.goToSibling('prev', true)
				break
			case '.':
				reader.goToSibling('next', true)
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

const HEIGHT_ZONE = 0.2

function getClickZone(e: MouseEvent): ClickZone {
	const target = e.currentTarget as HTMLElement
	const rect = target.getBoundingClientRect()

	// Vertical: use visible viewport portion (accounts for navbar/scroll)
	const visibleTop = Math.max(0, rect.top)
	const visibleBottom = Math.min(window.innerHeight, rect.bottom)
	const visibleHeight = visibleBottom - visibleTop
	const relativeY = e.clientY - visibleTop
	const heightPercent = relativeY / visibleHeight

	// Horizontal: use element bounds (accounts for sidebar)
	const centerWidth = Math.min(rect.width / 3, 300)
	const widthZone1 = (rect.width - centerWidth) / 2
	const widthZone2 = widthZone1 + centerWidth
	const relativeX = e.clientX - rect.left

	// Top 20% = prev
	if (heightPercent < HEIGHT_ZONE) {
		return 'prev'
	}
	// Bottom 20% = next
	if (heightPercent > 1 - HEIGHT_ZONE) {
		return 'next'
	}
	// Left third = prev
	if (relativeX < widthZone1) {
		return 'prev'
	}
	// Right third = next
	if (relativeX > widthZone2) {
		return 'next'
	}
	// Center = menu
	return 'menu'
}
