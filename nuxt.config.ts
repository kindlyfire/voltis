// https://nuxt.com/docs/api/configuration/nuxt-config
export default defineNuxtConfig({
	modules: ['@nuxt/ui'],
	srcDir: 'src',
	devtools: { enabled: true },
	typescript: {
		shim: false
	},
	app: {
		head: {
			link: []
		}
	},
	build: {
		transpile: ['trpc-nuxt']
	},
	vite: {
		build: {
			rollupOptions: {
				external: ['sqlite3', 'sequelize']
			}
		}
	}
})
