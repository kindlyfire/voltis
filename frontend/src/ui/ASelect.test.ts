import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { nextTick } from 'vue'
import { addOverlays } from '@/utils/modalTesting'
import ASelect from './ASelect.vue'

const props = { modelValue: 'a', options: ['a', 'b'], label: 'Status', clearable: true }
const global = { stubs: { ATooltip: { template: '<slot />' } } }

beforeEach(addOverlays)
afterEach(() => {
    document.body.innerHTML = ''
})

async function openWithKeyboard(wrapper: ReturnType<typeof mount>) {
    await wrapper.find('[role="combobox"]').trigger('keydown', { key: 'ArrowDown' })
    await nextTick()
    return !!document.querySelector('[role="listbox"]')
}

describe('ASelect', () => {
    it('clears to null', async () => {
        const wrapper = mount(ASelect, { props, global, attachTo: document.body })
        await wrapper.find('button[aria-label="Clear Status"]').trigger('click')
        expect(wrapper.emitted('update:modelValue')).toEqual([[null]])
        wrapper.unmount()
    })

    it('opens normally, but readonly neither opens nor clears', async () => {
        const editable = mount(ASelect, { props, global, attachTo: document.body })
        expect(await openWithKeyboard(editable)).toBe(true)
        editable.unmount()

        const readonly = mount(ASelect, {
            props: { ...props, readonly: true },
            global,
            attachTo: document.body,
        })
        expect(readonly.find('button[aria-label="Clear Status"]').exists()).toBe(false)
        expect(await openWithKeyboard(readonly)).toBe(false)
        expect(readonly.text()).toContain('a')
        readonly.unmount()
    })
})
