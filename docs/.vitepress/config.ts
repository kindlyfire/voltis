import { defineConfig } from 'vitepress'

// https://vitepress.dev/reference/site-config
export default defineConfig({
	title: 'Voltis',
	description: 'Comics and books in one place',
	themeConfig: {
		// https://vitepress.dev/reference/default-theme-config
		nav: [{ text: 'Home', link: '/' }],

		sidebar: [
			{
				text: 'Installation',
				link: '/installation',
			},
			// {
			// 	text: 'Installation',
			// 	items: [{ text: 'Markdown Examples', link: '/markdown-examples' }],
			// },
		],

		socialLinks: [{ icon: 'github', link: 'https://github.com/kindlyfire/voltis' }],
	},
})
