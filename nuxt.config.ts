// https://nuxt.com/docs/api/configuration/nuxt-config
export default defineNuxtConfig({
	runtimeConfig: {
		sessionCookieName: 'voltis_session',
		registrationsEnabled: false,
		guestAccess: false
	},
	modules: ['@nuxt/ui', '@pinia/nuxt'],
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
				external: ['sqlite3', 'sequelize', 'sharp']
			}
		}
	}
})
