import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { h } from 'vue'
import AButton from './AButton.vue'

describe('AButton', () => {
    it('blocks activation while loading but stays focusable and named', async () => {
        const onClick = vi.fn()
        const wrapper = mount(AButton, {
            props: { loading: true, onClick },
            slots: { default: 'Save' },
        })
        await wrapper.trigger('click')
        expect(onClick).not.toHaveBeenCalled()
        expect(wrapper.attributes('aria-busy')).toBe('true')
        expect(wrapper.attributes('disabled')).toBeUndefined()
        expect(wrapper.text()).toContain('Save')

        await wrapper.setProps({ loading: false })
        await wrapper.trigger('click')
        expect(onClick).toHaveBeenCalledTimes(1)
    })

    it('does not submit its form while loading', async () => {
        const onSubmit = vi.fn((e: Event) => e.preventDefault())
        const wrapper = mount(
            {
                render: () =>
                    h('form', { onSubmit }, [h(AButton, { type: 'submit', loading: true })]),
            },
            { attachTo: document.body }
        )
        await wrapper.find('button').trigger('click')
        expect(onSubmit).not.toHaveBeenCalled()
        wrapper.unmount()
    })
})
