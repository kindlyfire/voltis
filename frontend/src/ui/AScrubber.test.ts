import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AScrubber from './AScrubber.vue'

const segments = ['a', 'b', 'c', 'd'].map((key, i) => ({
    label: key.toUpperCase(),
    bubble: key.toUpperCase(),
    start: i / 4,
}))
const tolerance = 1e-3

describe('AScrubber', () => {
    it('steps by segment from the keyboard anchor or the current position', async () => {
        const wrapper = mount(AScrubber, {
            props: { segments, modelValue: 0, label: 'Jump to', tolerance },
        })
        const slider = wrapper.get('[role="slider"]')
        const seeks = () => wrapper.emitted('seek')?.map(([p]) => p)

        await slider.trigger('keydown', { key: 'ArrowDown' })
        expect(seeks()).toEqual([0.25])

        // The scroll lands just short of the target: Down still moves on.
        await wrapper.setProps({ modelValue: 0.25 - tolerance / 2 })
        await slider.trigger('keydown', { key: 'ArrowDown' })
        expect(seeks()).toEqual([0.25, 0.5])
        expect(slider.attributes('aria-valuenow')).toBe('2')
        expect(slider.attributes('aria-valuetext')).toBe('C')

        await slider.trigger('keydown', { key: 'ArrowUp' })
        await slider.trigger('keydown', { key: 'End' })
        expect(seeks()).toEqual([0.25, 0.5, 0.25, 0.75])

        // Scrolled elsewhere: steps start from the segment under the position.
        await wrapper.setProps({ modelValue: 0.3 })
        await slider.trigger('keydown', { key: 'ArrowDown' })
        expect(seeks()!.at(-1)).toBe(0.5)
    })
})
