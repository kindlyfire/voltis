import DefaultTheme from 'vitepress/theme-without-fonts'
import type { Theme } from 'vitepress'
import '@fontsource-variable/figtree'
import '@fontsource-variable/source-serif-4'
import 'photoswipe/style.css'
import './custom.css'
import Layout from './Layout.vue'
import Screenshot from './Screenshot.vue'

export default {
	extends: DefaultTheme,
	Layout,
	enhanceApp({ app }) {
		app.component('Screenshot', Screenshot)
	},
} satisfies Theme
