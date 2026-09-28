import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { defineComponent, h, nextTick, ref } from 'vue'
import { addOverlays } from '@/utils/modalTesting'
import ADrawer from './ADrawer.vue'

beforeEach(addOverlays)
afterEach(() => {
    document.body.innerHTML = ''
})

const locked = () => document.documentElement.classList.contains('drawer-lock')

describe('ADrawer', () => {
    it('locks page scroll while any drawer is open, until the last closes or unmounts', async () => {
        const a = ref(true)
        const b = ref(false)
        const shown = ref(true)
        const wrapper = mount(
            defineComponent(() => () => [
                h(ADrawer, { open: a.value, 'onUpdate:open': (v: boolean) => (a.value = v) }),
                shown.value &&
                    h(ADrawer, { open: b.value, 'onUpdate:open': (v: boolean) => (b.value = v) }),
            ]),
            { attachTo: document.body }
        )
        expect(locked()).toBe(true)

        b.value = true
        await nextTick()
        a.value = false
        await nextTick()
        expect(locked()).toBe(true)

        shown.value = false
        await nextTick()
        expect(locked()).toBe(false)

        a.value = true
        await nextTick()
        expect(locked()).toBe(true)
        wrapper.unmount()
        expect(locked()).toBe(false)
    })

    it('closes on a horizontal drag toward its edge past 35% of its width', async () => {
        const wrapper = mount(ADrawer, { props: { open: true, side: 'right' } })
        await nextTick()
        const panel = document.querySelector<HTMLElement>('.a-drawer')!
        panel.getBoundingClientRect = () => ({ width: 300 }) as DOMRect

        function touch(type: string, x: number, y = 100) {
            const point = [{ clientX: x, clientY: y }] as unknown as Touch[]
            const e = new TouchEvent(type, {
                bubbles: true,
                cancelable: true,
                touches: type === 'touchend' ? [] : point,
                changedTouches: point,
            })
            panel.dispatchEvent(e)
            return e
        }
        const swipe = (...xs: [number, number][]) => {
            touch('touchstart', 100)
            const moves = xs.map(([x, y]) => touch('touchmove', x, y))
            return { moves, end: touch('touchend', xs.at(-1)![0]) }
        }

        const vertical = swipe([102, 120], [104, 160])
        expect(vertical.moves.some(e => e.defaultPrevented)).toBe(false)

        const short = swipe([106, 100], [108, 100])
        expect(short.moves.map(e => e.defaultPrevented)).toEqual([false, true])
        expect(short.end.defaultPrevented).toBe(false)
        expect(panel.style.getPropertyValue('--drawer-drag')).toBe('')

        const far = swipe([120, 100], [250, 100])
        expect(far.end.defaultPrevented).toBe(true)
        expect(panel.style.getPropertyValue('--drawer-drag')).toBe('130px')
        expect(wrapper.emitted('update:open')).toEqual([[false]])
        wrapper.unmount()
    })
})
