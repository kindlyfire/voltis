import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import APagination from './APagination.vue'
import { pageItems } from './pagination'

describe('pageItems', () => {
    it('compresses long ranges around the current page', () => {
        expect(pageItems(3, 5)).toEqual([1, 2, 3, 4, 5])
        expect(pageItems(2, 20)).toEqual([1, 2, 3, 4, 5, 'gap', 20])
        expect(pageItems(10, 20)).toEqual([1, 'gap', 9, 10, 11, 'gap', 20])
        expect(pageItems(19, 20)).toEqual([1, 'gap', 16, 17, 18, 19, 20])
    })
})

describe('APagination', () => {
    it('clamps the page when the length shrinks', async () => {
        const wrapper = mount(APagination, {
            props: { page: 9, length: 10 },
            global: { stubs: { ATooltip: { template: '<slot />' } } },
        })
        expect(wrapper.emitted('update:page')).toBeUndefined()
        await wrapper.setProps({ length: 4 })
        expect(wrapper.emitted('update:page')).toEqual([[4]])
    })
})
