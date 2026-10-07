import type { Plugin } from 'vite'

/** Tags each build with a random ID: a meta in index.html and build.json for the backend. */
export const buildIdPlugin = (): Plugin => {
    const id = crypto.randomUUID()
    return {
        name: 'build-id',
        apply: 'build',
        transformIndexHtml: () => [
            { tag: 'meta', attrs: { name: 'voltis-build', content: id }, injectTo: 'head' },
        ],
        generateBundle() {
            this.emitFile({ type: 'asset', fileName: 'build.json', source: JSON.stringify({ id }) })
        },
    }
}
