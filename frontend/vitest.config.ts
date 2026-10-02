import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import { playwright } from '@vitest/browser-playwright'
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
        restoreMocks: true,
        // Istanbul instruments in every browser; v8 coverage is Chromium-only.
        coverage: {
            provider: 'istanbul',
            include: ['src/**'],
            exclude: [
                '**/*.test.ts',
                '**/browserFixture.ts',
                '**/fakeNav.ts',
                '**/fakeReadingServer.ts',
            ],
        },
        projects: [
            {
                extends: true,
                test: {
                    name: 'unit',
                    environment: 'jsdom',
                    include: ['src/**/*.test.ts'],
                    exclude: ['src/**/*.browser.test.ts'],
                },
            },
            {
                extends: true,
                test: {
                    name: 'browser',
                    include: ['src/**/*.browser.test.ts'],
                    browser: {
                        enabled: true,
                        headless: true,
                        provider: playwright(),
                        instances: [{ browser: 'chromium' }, { browser: 'firefox' }],
                        viewport: { width: 1280, height: 800 },
                    },
                },
            },
        ],
    },
})
