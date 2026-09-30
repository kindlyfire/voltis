import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { ScanLead } from '@/stores/scans'
import ScanLeadCard from './ScanLeadCard.vue'

const counts = { added: 0, updated: 0, removed: 0 }

function render(lead: ScanLead) {
    return mount(ScanLeadCard, {
        props: { lead, label: 'Scan' },
        global: {
            stubs: {
                ATooltip: { props: ['text'], template: '<div :data-tip="text"><slot /></div>' },
            },
        },
    })
}

describe('ScanLeadCard', () => {
    it('says why a scan removed nothing, and only then', async () => {
        const why = '/comics lists no files (3 of 3 items missing)'
        const wrapper = render({
            state: 'done',
            outcome: 'cancelled',
            counts,
            removalsSuppressed: why,
        })
        expect(wrapper.text()).toContain('Cancelled')
        const note = wrapper.find('[data-tip]')
        expect(note.text()).toBe('Nothing removed')
        expect(note.attributes('data-tip')).toBe(why)

        await wrapper.setProps({ lead: { state: 'done', outcome: 'completed', counts } })
        expect(wrapper.text()).toContain('Done')
        expect(wrapper.find('[data-tip]').exists()).toBe(false)
    })
})
