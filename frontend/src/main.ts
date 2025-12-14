import './assets/main.css'
import 'vuetify/styles'
import '@mdi/font/css/materialdesignicons.css'

import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import { VueQueryPlugin } from '@tanstack/vue-query'
import { createHead } from '@unhead/vue/client'

import App from './App.vue'
import router from './router.ts'

const app = createApp(App)

app.use(createPinia())
app.use(router)
app.use(
	createVuetify({
		components,
		theme: {
			defaultTheme: window.matchMedia('(prefers-color-scheme: dark)').matches
				? 'dark'
				: 'light',
		},
	})
)
app.use(VueQueryPlugin)
app.use(createHead())

app.mount('#app')
