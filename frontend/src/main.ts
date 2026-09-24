import '@fontsource-variable/figtree'
import '@fontsource-variable/source-serif-4'
import './assets/main.css'
import { VueQueryPlugin } from '@tanstack/vue-query'
import { createHead } from '@unhead/vue/client'
import { createPinia } from 'pinia'
import { createApp } from 'vue'
import App from './App.vue'
import router from './router.ts'
import { queryClient } from './utils/misc.ts'

const app = createApp(App)

app.use(createPinia())
app.use(router)
app.use(VueQueryPlugin, {
    queryClient,
})
app.use(createHead())

app.mount('#app')
