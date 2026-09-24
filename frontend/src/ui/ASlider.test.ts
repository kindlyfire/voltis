import { mount } from '@vue/test-utils'
import { SliderRoot } from 'reka-ui'
import { describe, expect, it } from 'vitest'
import ASlider from './ASlider.vue'

// Reka measures the thumb.
globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
}

describe('ASlider', () => {
    it('maps the single-number model, commit and value text', async () => {
        const wrapper = mount(ASlider, {
            props: {
                modelValue: 2,
                label: 'Page',
                max: 9,
                formatValue: (v: number) => `${v + 1} of 10`,
            },
        })
        expect(wrapper.find('[role="slider"]').attributes('aria-valuetext')).toBe('3 of 10')

        const root = wrapper.findComponent(SliderRoot)
        root.vm.$emit('update:modelValue', [5])
        root.vm.$emit('valueCommit', [5])
        expect(wrapper.emitted('update:modelValue')).toEqual([[5]])
        expect(wrapper.emitted('commit')).toEqual([[5]])
    })
})
