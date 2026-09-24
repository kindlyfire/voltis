import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ASegmented from './ASegmented.vue'

describe('ASegmented', () => {
    it('stays selected when the selected option is clicked again', async () => {
        const wrapper = mount(ASegmented, {
            props: { modelValue: 'a', options: ['a', 'b'], label: 'Mode' },
        })
        const [a, b] = wrapper.findAll('button')
        await a!.trigger('click')
        expect(wrapper.emitted('update:modelValue')).toBeUndefined()
        await b!.trigger('click')
        expect(wrapper.emitted('update:modelValue')).toEqual([['b']])
    })
})
