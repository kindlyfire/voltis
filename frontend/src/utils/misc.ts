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
