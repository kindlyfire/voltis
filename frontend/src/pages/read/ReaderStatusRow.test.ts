import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import { readingApi } from '@/utils/api/reading'
import type { ReadingStatus, SeriesReadingInfo } from '@/utils/api/types'
import ReaderStatusRow from './ReaderStatusRow.vue'
import type { ReadingSync } from './readingSync'

vi.mock('@/utils/api/reading', () => ({ readingApi: { post: vi.fn(), get: vi.fn() } }))

const stubs = {
    AMenu: { template: '<div><slot name="trigger" /><slot /></div>' },
    AIconButton: { template: '<span />' },
    AMenuItem: { template: '<button class="item" @click="$emit(\'select\')"><slot /></button>' },
}

function fakeSync(
    status: ReadingStatus | null,
    series: Partial<SeriesReadingInfo> | null = null,
    tracking = true
) {
    return reactive({
        acked: { status },
        series: series && { id: 's_1', caught_up: false, ...series },
        tracking,
        command: vi.fn(async () => null),
        seriesCommand: vi.fn(async () => {}),
        resetAndReadAgain: vi.fn(async () => {}),
        trackProgress: vi.fn(),
    }) as unknown as ReadingSync
}

function render(sync: ReadingSync) {
    const wrapper = mount(ReaderStatusRow, { props: { sync }, global: { stubs } })
    const click = async (label: string) => {
        const button = wrapper.findAll('button').find(b => b.text() === label)
        if (!button) throw new Error(`no ${label} in ${wrapper.text()}`)
        await button.trigger('click')
        await flushPromises()
    }
    return { wrapper, click }
}

describe('ReaderStatusRow', () => {
    it('acts on the item through the sync', async () => {
        const sync = fakeSync('plan_to_read')
        const { wrapper, click } = render(sync)
        expect(wrapper.text()).toContain('Plan to Read')
        await click('Mark completed')
        expect(sync.command).toHaveBeenCalledWith({ op: 'mark_completed' })
        await click('Set to Reading')
        expect(sync.command).toHaveBeenCalledWith({ op: 'set_status', status: 'reading' })
        await click('Reset & read again')
        expect(sync.resetAndReadAgain).toHaveBeenCalled()
        expect(readingApi.post).not.toHaveBeenCalled()
    })

    it('offers to reset a completed item, and resume a held series', async () => {
        const completed = render(fakeSync('completed'))
        expect(completed.wrapper.text()).toContain('Completed')
        expect(completed.wrapper.text()).not.toContain('Mark completed')

        const sync = fakeSync('reading', { status: 'on_hold' })
        const held = render(sync)
        expect(held.wrapper.text()).toContain('Series is On Hold')
        await held.click('Resume series')
        expect(sync.seriesCommand).toHaveBeenCalledWith('reading')
    })

    it('shows a caught-up series and stopped tracking', async () => {
        expect(
            render(fakeSync('reading', { status: 'reading', caught_up: true })).wrapper.text()
        ).toContain('Reading · Caught up')
        const sync = fakeSync('on_hold', null, false)
        const { wrapper, click } = render(sync)
        expect(wrapper.text()).toContain('On Hold · not tracking')
        await click('Track progress')
        expect(sync.trackProgress).toHaveBeenCalled()
    })
})
