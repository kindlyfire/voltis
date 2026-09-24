import { fileURLToPath, URL } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'
import vueDevTools from 'vite-plugin-vue-devtools'
import { iconsPlugin } from './vite.icons.ts'

// https://vite.dev/config/
export default defineConfig({
    plugins: [vue(), vueDevTools(), tailwindcss(), iconsPlugin()],
    resolve: {
        alias: {
            '@': fileURLToPath(new URL('./src', import.meta.url)),
        },
    },
})
