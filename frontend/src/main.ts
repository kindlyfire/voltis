import './assets/main.css'
import 'vuetify/styles/core'
import 'vuetify/styles/colors'
import '@mdi/font/css/materialdesignicons.css'
import { VueQueryPlugin } from '@tanstack/vue-query'
import { createHead } from '@unhead/vue/client'
import { createPinia } from 'pinia'
import { createApp } from 'vue'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import App from './App.vue'
import router from './router.ts'
import { queryClient } from './utils/misc.ts'

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
app.use(VueQueryPlugin, {
    queryClient,
})
app.use(createHead())

app.mount('#app')
