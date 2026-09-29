import { type DOMWrapper, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import ScanModal from '@/pages/settings/ScanModal.vue'
import { useScanStore } from '@/stores/scans'
import { librariesApi } from '@/utils/api/libraries'
import { tasksApi } from '@/utils/api/tasks'
import type { ScanProgress, ScanRecent, TaskSnapshot } from '@/utils/api/types'

vi.mock('@/utils/api/tasks', () => ({
    tasksApi: { snapshot: vi.fn(), logs: vi.fn() },
}))

vi.mock('@/utils/api/libraries', () => ({
    librariesApi: { useList: vi.fn(), useScan: vi.fn() },
}))

vi.mock('@/utils/api/users', async () => {
    const { ref } = await import('vue')
    return { usersApi: { useMe: () => ({ data: ref({ permissions: ['ADMIN'] }) }) } }
})

vi.mock('@/utils/ws', () => ({
    ws: { connect: () => {}, send: () => {}, on: () => () => {} },
}))

const snapshotMock = vi.mocked(tasksApi.snapshot)
const logsMock = vi.mocked(tasksApi.logs)
const scanMock = vi.fn()

const stubs = { ADialog: { template: '<div><slot /><slot name="actions" /></div>' } }

function task(over: Partial<TaskSnapshot> = {}): TaskSnapshot {
    return {
        id: 't_1',
        name: 'scan_library',
        status: 1,
        input: { library_id: 'l_1', library_type: 'comics', sources: ['/comics'], force: false },
        output: {},
        progress: null,
        log_len: 0,
        created_at: '2026-09-18T08:00:00Z',
        updated_at: '2026-09-18T08:00:01Z',
        ...over,
    }
}

function recent(over: Partial<ScanRecent>): ScanRecent {
    return {
        id: 'c_1',
        title: 'Series',
        cover_version: 'abc',
        added: 0,
        updated: 0,
        removed: 0,
        deleted: false,
        ...over,
    }
}

const parsing: ScanProgress = {
    phase: 'parsing',
    found: 10,
    total: 8,
    processed: 3,
    unchanged: 2,
    failed: 0,
    saved: { added: 3, updated: 1, removed: 2 },
    commit_seq: 1,
}

// Each chip as its visible symbol and its screen reader label.
function chips(card: DOMWrapper<Element>): string[][] {
    return card
        .findAll('.a-chip')
        .map(c => [c.find('[aria-hidden="true"]').text(), c.find('.sr-only').text()])
}

function open() {
    return mount(ScanModal, {
        props: { open: true, close: () => {}, libraryIds: ['l_1'] },
        global: { stubs },
    })
}

async function startScan(wrapper: ReturnType<typeof open>) {
    const buttons = wrapper.findAll('button')
    await buttons[buttons.length - 1]!.trigger('click')
    await Promise.resolve()
    await Promise.resolve()
}

beforeEach(() => {
    setActivePinia(createPinia())
    snapshotMock.mockReset()
    logsMock.mockReset()
    scanMock.mockReset()
    snapshotMock.mockResolvedValue([])
    scanMock.mockResolvedValue({ task_ids: ['t_1'] })
    vi.mocked(librariesApi.useList).mockReturnValue({ data: ref([]) } as any)
    vi.mocked(librariesApi.useScan).mockReturnValue({
        mutateAsync: scanMock,
        isPending: ref(false),
        isError: ref(false),
        error: ref(null),
    } as any)
})

afterEach(() => {
    vi.useRealTimers()
})

describe('ScanModal', () => {
    it('reconciles the task IDs returned by the scan POST', async () => {
        const wrapper = open()
        await startScan(wrapper)

        expect(scanMock).toHaveBeenCalledWith({ ids: ['l_1'], force: false })
        expect(snapshotMock).toHaveBeenCalledWith(['t_1'])
        wrapper.unmount()
    })

    it('retries the log tail after a failed request', async () => {
        vi.useFakeTimers()
        const store = useScanStore()
        store.accept(task({ log_len: 5 }))
        logsMock.mockRejectedValueOnce(new Error('network'))
        logsMock.mockResolvedValue({ offset: 0, text: 'hello', len: 5 })

        const wrapper = open()
        await startScan(wrapper)
        await vi.advanceTimersByTimeAsync(0)
        expect(logsMock).toHaveBeenCalledTimes(1)

        await vi.advanceTimersByTimeAsync(2000)
        expect(logsMock).toHaveBeenCalledTimes(2)
        expect(store.logs['t_1']).toEqual({ text: 'hello', len: 5 })

        await vi.advanceTimersByTimeAsync(5000)
        expect(logsMock).toHaveBeenCalledTimes(2)
        wrapper.unmount()
    })

    it('clears its poll timer when closed and when unmounted', async () => {
        vi.useFakeTimers()
        const store = useScanStore()
        store.accept(task({ log_len: 5 }))
        logsMock.mockRejectedValue(new Error('network'))

        const wrapper = open()
        await startScan(wrapper)
        await vi.advanceTimersByTimeAsync(2000)
        const calls = logsMock.mock.calls.length
        expect(vi.getTimerCount()).toBe(1)

        await wrapper.setProps({ open: false })
        expect(vi.getTimerCount()).toBe(0)

        await vi.advanceTimersByTimeAsync(5000)
        expect(logsMock.mock.calls.length).toBe(calls)

        await wrapper.setProps({ open: true })
        expect(vi.getTimerCount()).toBe(1)

        wrapper.unmount()
        expect(vi.getTimerCount()).toBe(0)
    })

    it('shows the lead card and the recent covers, newest first', async () => {
        const store = useScanStore()
        store.accept(
            task({
                progress: {
                    ...parsing,
                    recent: [
                        recent({ id: 'c_2', title: 'Fresh', added: 3, updated: 1 }),
                        recent({
                            id: 'c_1',
                            title: 'Gone',
                            cover_version: null,
                            removed: 2,
                            deleted: true,
                        }),
                    ],
                },
            })
        )

        const wrapper = open()
        await startScan(wrapper)

        const cards = wrapper.findAll('.scan-strip > li')
        expect(cards).toHaveLength(3)
        expect(cards[0]!.text()).toContain('5 files left')
        expect(cards[0]!.find('[role="progressbar"]').attributes('aria-valuenow')).toBe('38')

        expect(cards[1]!.find('img').attributes('src')).toMatch(/\/files\/cover\/c_2\?v=abc$/)
        expect(cards[1]!.text()).toContain('Fresh')
        expect(chips(cards[1]!)).toEqual([
            ['+3', '3 added'],
            ['~1', '1 updated'],
        ])

        expect(cards[2]!.find('img').exists()).toBe(false)
        expect(cards[2]!.find('.a-cover__placeholder').exists()).toBe(true)
        expect(cards[2]!.text()).toContain('Gone')
        expect(chips(cards[2]!)).toEqual([['−2', '2 removed']])
        wrapper.unmount()
    })

    it('shows the counts a failed scan committed on its lead card', async () => {
        const store = useScanStore()
        store.accept(task({ status: 3, progress: parsing }))

        const wrapper = open()
        await startScan(wrapper)

        const lead = wrapper.find('.scan-strip > li')
        expect(lead.text()).toContain('Failed')
        expect(chips(lead)).toEqual([
            ['+3', '3 added'],
            ['~1', '1 updated'],
            ['−2', '2 removed'],
        ])
        wrapper.unmount()
    })

    it('shows every count on a completed lead card, zeros included', async () => {
        const store = useScanStore()
        store.accept(
            task({
                status: 2,
                progress: parsing,
                output: { added: 0, updated: 4, removed: 0, failed: 0, unchanged: 3, duration: 1 },
            })
        )

        const wrapper = open()
        await startScan(wrapper)

        const lead = wrapper.find('.scan-strip > li')
        expect(lead.text()).toContain('Done')
        expect(chips(lead)).toEqual([
            ['+0', '0 added'],
            ['~4', '4 updated'],
            ['−0', '0 removed'],
        ])
        wrapper.unmount()
    })

    it('follows the log only while it is scrolled to the end', async () => {
        const store = useScanStore()
        store.accept(task())
        store.logs['t_1'] = { text: 'one\n', len: 4 }

        const wrapper = open()
        await startScan(wrapper)
        const pre = wrapper.find('pre').element
        // Ten pixels per character, so the height follows the rendered text.
        Object.defineProperty(pre, 'scrollHeight', { get: () => pre.textContent!.length * 10 })
        Object.defineProperty(pre, 'clientHeight', { value: 20 })
        pre.scrollTop = 10

        store.logs['t_1'] = { text: 'one\ntwo\n', len: 8 }
        await nextTick()
        await nextTick()
        expect(pre.scrollTop).toBe(70)

        pre.scrollTop = 0
        store.logs['t_1'] = { text: 'one\ntwo\nthree\n', len: 14 }
        await nextTick()
        await nextTick()
        expect(pre.scrollTop).toBe(0)
        wrapper.unmount()
    })
})
