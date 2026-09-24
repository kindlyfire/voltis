import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'
import { iconsPlugin } from './vite.icons.ts'

export default defineConfig({
    plugins: [vue(), iconsPlugin()],
    resolve: {
        alias: {
            '@': fileURLToPath(new URL('./src', import.meta.url)),
        },
    },
    test: {
        environment: 'jsdom',
        include: ['src/**/*.test.ts'],
        restoreMocks: true,
    },
})
