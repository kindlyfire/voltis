function color(varname) {
	return `rgb(var(--c-${varname}) / <alpha-value>)`
}

/** @type {import('tailwindcss').Config} */
module.exports = {
	content: ['src/**/*.vue'],
	theme: {
		extend: {
			colors: {
				'c-text': color('text'),
				'c-text-muted': color('text-muted'),
				muted: color('text-muted'),
				'c-bg': color('bg'),
				'c-bg-2': color('bg-2'),
				'c-bg-3': color('bg-3')
			}
		}
	},
	plugins: []
}
