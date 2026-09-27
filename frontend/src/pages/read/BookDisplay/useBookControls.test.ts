import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { getClickZone } from '../useClickZones'
import type { BookSession } from './createBookSession'
import { createTapGuard, useBookControls } from './useBookControls'
import { useBookDisplayStore } from './useBookDisplayStore'

const modal = vi.hoisted(() => ({ value: false }))
vi.mock('@/utils/modals', async original => ({
    ...(await original<typeof import('@/utils/modals')>()),
    hasOpenModal: modal,
}))

type TapGuard = ReturnType<typeof createTapGuard>

function selection(text: string) {
    vi.spyOn(window, 'getSelection').mockReturnValue({
        toString: () => text,
    } as unknown as Selection)
}

/** `composedPath()` is only populated while the event is being dispatched, so
 * the guard has to be asked from inside a listener, as it is in the reader. */
function tap(guard: TapGuard, target: Element, init: MouseEventInit = {}) {
    let allowed: boolean | null = null
    root.addEventListener(
        'click',
        e => {
            allowed = guard.allows(e)
            // jsdom refuses to follow the external link otherwise.
            e.preventDefault()
        },
        { once: true }
    )
    target.dispatchEvent(
        new MouseEvent('click', { bubbles: true, composed: true, cancelable: true, ...init })
    )
    return allowed
}

let root: HTMLElement
let link: HTMLElement

beforeEach(() => {
    selection('')
    // As `rewriteAnchor` leaves them: in-book links keep an href to the
    // reader, off-book ones lose it, external ones keep their own.
    document.body.innerHTML = `<div id="root">
        <a id="link" data-book-href="a.xhtml" data-book-frag="" href="/r/c_1?ch=a.xhtml">x</a>
        <a id="external" href="https://example.com" target="_blank">x</a>
        <a id="missing" data-book-missing="1">x</a>
    </div>`
    root = document.getElementById('root')!
    link = document.getElementById('link')!
})

describe('tap guard', () => {
    it('allows a plain tap', () => {
        const guard = createTapGuard()
        guard.down(new MouseEvent('pointerdown', { clientX: 10, clientY: 10 }) as PointerEvent)
        expect(tap(guard, root, { clientX: 12, clientY: 14 })).toBe(true)
    })

    it('rejects a click that ends a text selection', () => {
        selection('a selected passage')
        const guard = createTapGuard()
        expect(tap(guard, root)).toBe(false)
    })

    it('rejects a click that only dismisses a selection', () => {
        const guard = createTapGuard()
        selection('a selected passage')
        guard.down(new MouseEvent('pointerdown', { clientX: 10, clientY: 10 }) as PointerEvent)
        // The press collapses it, so the click itself sees nothing selected.
        selection('')
        expect(tap(guard, root, { clientX: 10, clientY: 10 })).toBe(false)
    })

    it('rejects a click on any kind of link', () => {
        const guard = createTapGuard()
        expect(tap(guard, link)).toBe(false)
        expect(tap(guard, document.getElementById('external')!)).toBe(false)
        expect(tap(guard, document.getElementById('missing')!)).toBe(false)
    })

    it('rejects a click the session already handled', () => {
        const guard = createTapGuard()
        root.addEventListener('click', e => e.preventDefault(), { once: true })
        expect(tap(guard, root)).toBe(false)
    })

    it('rejects a drag, then judges the next click on its own', () => {
        const guard = createTapGuard()
        guard.down(new MouseEvent('pointerdown', { clientX: 10, clientY: 10 }) as PointerEvent)
        expect(tap(guard, root, { clientX: 10, clientY: 40 })).toBe(false)
        expect(tap(guard, root, { clientX: 10, clientY: 40 })).toBe(true)
    })
})

describe('click zones', () => {
    function zone(x: number, y: number) {
        const target = {
            getBoundingClientRect: () => ({ top: 0, bottom: 800, left: 0, width: 900 }),
        }
        return getClickZone({
            currentTarget: target,
            clientX: x,
            clientY: y,
        } as unknown as MouseEvent)
    }

    it('splits the viewport the way the comic reader does', () => {
        window.innerHeight = 800
        expect(zone(450, 40)).toBe('prev')
        expect(zone(450, 760)).toBe('next')
        expect(zone(50, 400)).toBe('prev')
        expect(zone(850, 400)).toBe('next')
        expect(zone(450, 400)).toBe('menu')
    })
})

describe('paged controls', () => {
    let wrapper: ReturnType<typeof mount> | null = null
    afterEach(() => wrapper?.unmount())

    function setup(layoutMode: 'paged' | 'scroll' = 'paged') {
        const pinia = createPinia()
        setActivePinia(pinia)
        const router = createRouter({ history: createMemoryHistory(), routes: [] })
        const store = useBookDisplayStore()
        const turn = vi.fn()
        const goToChapter = vi.fn()
        store.session = {
            layoutMode,
            turn,
            goToChapter,
            chapterIndex: 1,
            standalone: null,
        } as unknown as BookSession
        let controls!: ReturnType<typeof useBookControls>
        wrapper = mount(
            defineComponent({
                setup() {
                    controls = useBookControls()
                    return () => h('div')
                },
            }),
            { global: { plugins: [pinia, router] }, attachTo: document.body }
        )
        return { store, turn, goToChapter, controls }
    }

    const key = (name: string, init: KeyboardEventInit = {}) =>
        window.dispatchEvent(new KeyboardEvent('keydown', { key: name, ...init }))

    function click(controls: ReturnType<typeof useBookControls>, x: number) {
        const target = document.createElement('div')
        target.getBoundingClientRect = () =>
            ({ top: 0, bottom: 800, left: 0, width: 900 }) as DOMRect
        target.addEventListener('click', e => controls.handleClick(e), { once: true })
        target.dispatchEvent(new MouseEvent('click', { clientX: x, clientY: 400, bubbles: true }))
    }

    function touch(x: number, time: number, lifted = false) {
        const point = [{ clientX: x, clientY: 0 }]
        return {
            touches: lifted ? [] : point,
            changedTouches: point,
            timeStamp: time,
            composedPath: () => [],
        } as unknown as TouchEvent
    }

    beforeEach(() => {
        modal.value = false
        window.innerHeight = 800
        Object.defineProperty(window, 'visualViewport', { value: { scale: 1 }, configurable: true })
    })

    it('turns pages from the keyboard', () => {
        const { turn } = setup()
        for (const name of ['ArrowRight', 'ArrowDown', 'PageDown', ' ']) key(name)
        for (const name of ['ArrowLeft', 'ArrowUp', 'PageUp']) key(name)
        key(' ', { shiftKey: true })
        expect(turn.mock.calls.map(call => call[0])).toEqual([
            ...Array(4).fill('next'),
            ...Array(4).fill('prev'),
        ])
    })

    it('scrolls the window in scroll mode, crossing chapters at its edges', () => {
        window.scrollBy = vi.fn()
        const height = (value: number) =>
            Object.defineProperty(document.documentElement, 'scrollHeight', {
                value,
                configurable: true,
            })
        height(5000)
        const { turn, goToChapter } = setup('scroll')
        key('ArrowDown')
        expect(window.scrollBy).toHaveBeenCalledOnce()
        // At the top, back into the previous chapter, landing at its end.
        key('ArrowUp')
        height(800)
        key('ArrowDown')
        expect(goToChapter.mock.calls).toEqual([
            [0, true],
            [2, false],
        ])
        expect(turn).not.toHaveBeenCalled()
        Reflect.deleteProperty(document.documentElement, 'scrollHeight')
    })

    it('turns on zone taps, except while zoomed in', () => {
        const { turn, controls } = setup()
        click(controls, 850)
        click(controls, 50)
        expect(turn.mock.calls).toEqual([['next'], ['prev']])
        Object.defineProperty(window, 'visualViewport', { value: { scale: 2 }, configurable: true })
        click(controls, 850)
        expect(turn).toHaveBeenCalledTimes(2)
    })

    it('keeps a quick click after a turn from selecting the word under it', () => {
        const { controls } = setup()
        const press = (detail: number) => {
            const e = new MouseEvent('mousedown', { detail, cancelable: true })
            controls.handleMouseDown(e)
            return e.defaultPrevented
        }
        expect(press(2)).toBe(false)
        click(controls, 850)
        expect(press(1)).toBe(false)
        expect(press(2)).toBe(true)
        // After the menu zone, a double-click still selects.
        click(controls, 450)
        expect(press(2)).toBe(false)
    })

    it('turns on wheel and swipe, but not under the drawer or a modal', () => {
        const { store, turn, controls } = setup()
        const gesture = (at: number) => {
            controls.handleWheel(new WheelEvent('wheel', { deltaY: 100 }))
            controls.handleTouchStart(touch(300, at))
            controls.handleTouchEnd(touch(100, at + 150, true))
        }
        gesture(0)
        expect(turn.mock.calls).toEqual([['next'], ['next']])
        store.sidebarOpen = true
        gesture(1000)
        store.sidebarOpen = false
        modal.value = true
        gesture(2000)
        expect(turn).toHaveBeenCalledTimes(2)
    })
})
