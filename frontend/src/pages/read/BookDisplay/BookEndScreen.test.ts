import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick, ref } from 'vue'
import type { Content } from '@/utils/api/types'
import BookEndScreen from './BookEndScreen.vue'

const stubs = { AButton: { template: '<button v-bind="$attrs"><slot /></button>' } }
const NEXT = { id: 'c_2', title: 'Volume 2' } as Content

function render(next: Content | null) {
    return mount(BookEndScreen, {
        props: { contentId: 'c_1', next },
        global: { stubs },
        attachTo: document.body,
    })
}

describe('BookEndScreen', () => {
    it('focuses the next volume and opens it', async () => {
        const wrapper = render(NEXT)
        const next = wrapper.find('.book-end__next')
        expect(document.activeElement).toBe(next.element)
        await next.trigger('click')
        expect(wrapper.emitted('open')).toEqual([['c_2']])
        wrapper.unmount()
    })

    it('focuses the heading without one, then the volume once it arrives', async () => {
        const wrapper = render(null)
        expect(document.activeElement).toBe(wrapper.find('h2').element)
        await wrapper.setProps({ next: NEXT })
        await wrapper.vm.$nextTick()
        expect(document.activeElement).toBe(wrapper.find('.book-end__next').element)
        wrapper.unmount()
    })

    it('hands focus on when it closes with focus inside', async () => {
        const onBlur = vi.fn()
        const shown = ref(true)
        const wrapper = mount(
            defineComponent({
                setup: () => () =>
                    shown.value ? h(BookEndScreen, { contentId: 'c_1', next: NEXT, onBlur }) : null,
            }),
            { global: { stubs }, attachTo: document.body }
        )
        shown.value = false
        await nextTick()
        expect(onBlur).toHaveBeenCalledOnce()
        wrapper.unmount()
    })
})
