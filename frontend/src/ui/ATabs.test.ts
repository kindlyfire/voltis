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

    it('shares one panel that follows the selection, and keeps it when tabs appear', async () => {
        const wrapper = mount(ATabs, {
            props: { modelValue: 'a', options: [], label: 'Sections', shared: true },
            slots: { default: '<p id="body">Body</p>' },
            attachTo: document.body,
        })
        const body = wrapper.find('#body').element
        expect(wrapper.find('[role=tablist]').exists()).toBe(false)
        expect(wrapper.find('[role=tabpanel]').exists()).toBe(false)

        await wrapper.setProps({
            options: [
                { value: 'a', label: 'Alpha' },
                { value: 'b', label: 'Beta' },
            ],
        })
        const panels = wrapper.findAll('[role=tabpanel]')
        expect(panels).toHaveLength(1)
        expect(wrapper.find('#body').element).toBe(body)
        const [tabA, tabB] = wrapper.findAll('[role=tab]')
        expect(panels[0]!.attributes('aria-labelledby')).toBe(tabA!.attributes('id'))

        await wrapper.setProps({ modelValue: 'b' })
        const panel = wrapper.find('[role=tabpanel]')
        expect(panel.attributes('aria-labelledby')).toBe(tabB!.attributes('id'))
        for (const tab of [tabA!, tabB!])
            expect(tab.attributes('aria-controls')).toBe(panel.attributes('id'))
        expect(wrapper.find('#body').element).toBe(body)
    })
})
