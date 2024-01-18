import { readerKey } from './use-reader'

interface UseReaderActionsOptions {
	onNext(): void
	onBack(): void
}

enum ReaderAction {
	Menu,
	Previous,
	Next
}
const HEIGHT_ZONE = 0.2

export function useReaderActions(options: UseReaderActionsOptions) {
	const reader = inject(readerKey)!

	const clickListener = (e: MouseEvent) => {
		const heightPercent = e.clientY / window.innerHeight
		const centerWidth = Math.min(window.innerWidth / 3, 300)
		const widthZone1 = (window.innerWidth - centerWidth) / 2
		const widthZone2 = widthZone1 + centerWidth

		let action = ReaderAction.Menu
		if (heightPercent < HEIGHT_ZONE) {
			action = ReaderAction.Previous
		} else if (heightPercent > 1 - HEIGHT_ZONE) {
			action = ReaderAction.Next
		} else if (e.clientX < widthZone1) {
			action = ReaderAction.Previous
		} else if (e.clientX > widthZone2) {
			action = ReaderAction.Next
		}

		if (action === ReaderAction.Previous) {
			options.onBack()
		} else if (action === ReaderAction.Next) {
			options.onNext()
		} else {
			reader.state.menuOpen = true
		}
	}

	watch(
		() => reader.state.mainRef,
		(r, oldR) => {
			if (oldR) oldR.removeEventListener('click', clickListener)
			if (r) r.addEventListener('click', clickListener)
		},
		{ immediate: true }
	)

	onUnmounted(() => {
		reader.state.mainRef?.removeEventListener('click', clickListener)
	})
}
