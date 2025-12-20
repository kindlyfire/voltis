export function getScrollParent(element: HTMLElement): HTMLElement | null {
	let parent: HTMLElement | null = element
	while (parent) {
		const { overflow, overflowY, overflowX } = window.getComputedStyle(parent)
		if (/(auto|scroll)/.test(overflow + overflowY + overflowX)) {
			// Check if element actually has scrollable content
			const hasVerticalScroll = parent.scrollHeight > parent.clientHeight
			const hasHorizontalScroll = parent.scrollWidth > parent.clientWidth

			if (hasVerticalScroll || hasHorizontalScroll) {
				return parent
			}
		}

		parent = parent.parentElement
	}
	return null
}
