// https://nuxt.com/docs/api/configuration/nuxt-config
export default defineNuxtConfig({
	runtimeConfig: {
		dataDir: 'data',
		sessionCookieName: 'voltis_session',
		registrationsEnabled: false,
		guestAccess: false,
		public: {
			brand: 'Voltis'
		}
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
				external: ['sqlite3', 'sharp', 'bcrypt', 'pg']
			}
		}
	}
})
