import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import ScanModal from '@/pages/settings/ScanModal.vue'
import { useScanStore } from '@/stores/scans'
import { librariesApi } from '@/utils/api/libraries'
import { tasksApi } from '@/utils/api/tasks'
import type { TaskSnapshot } from '@/utils/api/types'

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

const pass = { template: '<div><slot /></div>' }
const stubs = {
    VDialog: pass,
    VCard: pass,
    VCardTitle: pass,
    VCardText: pass,
    VCheckbox: { template: '<div />' },
    VProgressCircular: { template: '<div />' },
    VProgressLinear: { template: '<div />' },
    VBtn: { template: '<button><slot /></button>' },
}

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
    vi.mocked(librariesApi.useScan).mockReturnValue({ mutateAsync: scanMock } as any)
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
})
