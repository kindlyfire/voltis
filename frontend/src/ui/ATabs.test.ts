import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import ATabs from './ATabs.vue'

afterEach(() => {
    document.body.innerHTML = ''
})

function mountTabs() {
    const select = vi.fn()
    const wrapper = mount(ATabs, {
        props: {
            modelValue: 'a',
            'onUpdate:modelValue': select,
            options: [
                { value: 'a', label: 'Alpha' },
                { value: 'b', label: 'Beta' },
            ],
            label: 'Sections',
        },
        slots: { a: '<input id="in-a" />', b: '<p>Panel B</p>' },
        attachTo: document.body,
    })
    select.mockImplementation(modelValue => wrapper.setProps({ modelValue }))
    return wrapper
}

describe('ATabs', () => {
    it('selects a tab on click, keeping every panel mounted', async () => {
        const wrapper = mountTabs()
        const [a, b] = wrapper.findAll('[role=tab]')
        const panels = () => wrapper.findAll<HTMLElement>('[role=tabpanel]')
        expect(a!.attributes('aria-selected')).toBe('true')
        const shown = () => panels().map(p => !p.element.hidden)
        expect(shown()).toEqual([true, false])

        wrapper.find<HTMLInputElement>('#in-a').element.value = 'kept'
        await b!.trigger('mousedown', { button: 0 })
        expect(wrapper.props('modelValue')).toBe('b')
        expect(shown()).toEqual([false, true])
        expect(wrapper.find<HTMLInputElement>('#in-a').element.value).toBe('kept')
        expect(wrapper.text()).toContain('Panel B')
    })

    it('moves between tabs with the arrow keys', async () => {
        const wrapper = mountTabs()
        const [a, b] = wrapper.findAll('[role=tab]')
        ;(a!.element as HTMLElement).focus()
        await nextTick()
        await a!.trigger('keydown', { key: 'ArrowRight' })
        expect(document.activeElement).toBe(b!.element)
        expect(wrapper.props('modelValue')).toBe('b')
        await b!.trigger('keydown', { key: 'ArrowRight' })
        await nextTick()
        expect(wrapper.props('modelValue')).toBe('a')
    })

    it('exposes each panel element', () => {
        const wrapper = mountTabs()
        const panels = wrapper.findAll('[role=tabpanel]')
        expect(wrapper.vm.panel('a')).toBe(panels[0]!.element)
        expect(wrapper.vm.panel('b')).toBe(panels[1]!.element)
    })
})
