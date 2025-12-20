import { onMounted, onUnmounted } from 'vue'
import { useReaderStore } from './use-reader-store'

type ClickZone = 'prev' | 'next' | 'menu'

export function useReaderControls() {
	const reader = useReaderStore()

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
				reader.handlePrev()
				break
			case 'ArrowRight':
				reader.handleNext()
				break
			case ',':
				if (reader.prevSibling) {
					reader.goToSibling(reader.prevSibling.id, true)
				}
				break
			case '.':
				if (reader.nextSibling) {
					reader.goToSibling(reader.nextSibling.id)
				}
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
				reader.handlePrev()
			} else if (zone === 'next') {
				reader.handleNext()
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
