import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick, ref } from 'vue'
import { useChapterScrollProgress } from './useChapterScrollProgress'

const HOST_TOP = 100
let scrollY = 0
let hostHeight = 3000

class FakeResizeObserver {
    constructor(private callback: ResizeObserverCallback) {}
    observe() {
        this.callback(
            [{ contentRect: { width: 600, height: hostHeight } } as ResizeObserverEntry],
            this as unknown as ResizeObserver
        )
    }
    unobserve() {}
    disconnect() {}
}

function setup() {
    let progress!: ReturnType<typeof useChapterScrollProgress>
    const wrapper = mount(
        defineComponent({
            setup() {
                const host = ref<HTMLElement>()
                progress = useChapterScrollProgress(host)
                return () => h('div', { ref: host })
            },
        }),
        { attachTo: document.body }
    )
    return { wrapper, progress }
}

async function scrollTo(y: number) {
    scrollY = y
    document.documentElement.scrollTop = y
    window.dispatchEvent(new Event('scroll'))
    await nextTick()
}

beforeEach(() => {
    scrollY = 0
    hostHeight = 3000
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)
    Object.defineProperty(window, 'innerHeight', { value: 1000, configurable: true })
    Object.defineProperty(window, 'scrollY', { get: () => scrollY, configurable: true })
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(
        () => ({ top: HOST_TOP - scrollY, height: hostHeight }) as DOMRect
    )
})

describe('chapter scroll progress', () => {
    it('runs from 0 at the chapter top to 1 at its bottom', async () => {
        const { wrapper, progress } = setup()
        await nextTick()
        expect(progress.value).toBe(0)
        await scrollTo(HOST_TOP + 1000)
        expect(progress.value).toBe(0.5)
        await scrollTo(HOST_TOP + 2500)
        expect(progress.value).toBe(1)
        wrapper.unmount()
    })

    it('is complete when the chapter fits in the window', async () => {
        hostHeight = 800
        const { wrapper, progress } = setup()
        await nextTick()
        expect(progress.value).toBe(1)
        wrapper.unmount()
    })
})
