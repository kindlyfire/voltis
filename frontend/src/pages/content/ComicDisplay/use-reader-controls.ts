import { type ReaderState } from './use-reader'

type ClickZone = 'prev' | 'next' | 'menu'

export function useReaderControls(reader: ReaderState) {
	return {
		handleClick(e: MouseEvent) {
			const zone = getClickZone(e)
			if (zone === 'prev') {
				reader.handlePrev()
			} else if (zone === 'next') {
				reader.handleNext()
			} else {
				reader.sidebarOpen.value = true
			}
		},
	}
}

const HEIGHT_ZONE = 0.2

function getClickZone(e: MouseEvent): ClickZone {
	const target = e.currentTarget as HTMLElement
	const rect = target.getBoundingClientRect()
	const relativeY = e.clientY - rect.top
	const heightPercent = relativeY / rect.height

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
