export const LIBRARY_GRID_CLASSES =
	'grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-6 3xl:grid-cols-8 gap-4'

export function getLayoutTop() {
	try {
		return parseInt(
			getComputedStyle(document.getElementsByClassName('v-main')[0]!).getPropertyValue(
				'--v-layout-top'
			) || '0'
		)
	} catch {
		return 0
	}
}
