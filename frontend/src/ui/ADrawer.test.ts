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
})
