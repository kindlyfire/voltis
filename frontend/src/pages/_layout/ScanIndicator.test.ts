import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import ScanIndicator from '@/pages/_layout/ScanIndicator.vue'
import { useScanStore } from '@/stores/scans'
import { librariesApi } from '@/utils/api/libraries'
import type { TaskSnapshot, TaskStatusValue } from '@/utils/api/types'

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

const stubs = {
    APopover: { template: '<div><slot name="trigger" /><slot /></div>', emits: ['update:open'] },
    AIconButton: true,
}

function task(id: string, status: TaskStatusValue): TaskSnapshot {
    return {
        id,
        name: 'scan_library',
        status,
        input: { library_id: 'l_' + id, library_type: 'comics', sources: ['/c'], force: false },
        output:
            status >= 2
                ? { added: 0, updated: 0, removed: 0, failed: 0, unchanged: 0, duration: 1 }
                : {},
        progress: null,
        log_len: 0,
        created_at: '2026-09-18T08:00:00Z',
        updated_at: new Date().toISOString(),
    }
}

beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(librariesApi.useList).mockReturnValue({ data: ref([]) } as any)
    vi.useFakeTimers()
})

afterEach(() => {
    vi.useRealTimers()
})

function open() {
    return mount(ScanIndicator, { global: { stubs } })
}

describe('ScanIndicator dismissal', () => {
    it('dismisses a task that was already terminal at mount', async () => {
        const store = useScanStore()
        store.accept(task('t_1', 2))

        const wrapper = open()
        await vi.advanceTimersByTimeAsync(10_000)

        expect(store.dismissed.has('t_1')).toBe(true)
        wrapper.unmount()
    })

    it('gives each terminal task its own ten seconds', async () => {
        const store = useScanStore()
        store.accept(task('t_1', 1))

        const wrapper = open()
        store.accept(task('t_1', 2))
        await nextTick()

        await vi.advanceTimersByTimeAsync(5_000)
        store.accept(task('t_2', 2))
        await nextTick()

        await vi.advanceTimersByTimeAsync(5_000)
        expect(store.dismissed.has('t_1')).toBe(true)
        expect(store.dismissed.has('t_2')).toBe(false)

        await vi.advanceTimersByTimeAsync(5_000)
        expect(store.dismissed.has('t_2')).toBe(true)
        wrapper.unmount()
    })

    it('never dismisses a scan that started after the timer was armed', async () => {
        const store = useScanStore()
        store.accept(task('t_1', 2))

        const wrapper = open()
        await nextTick()
        wrapper.findComponent(stubs.APopover).vm.$emit('update:open', true)
        await nextTick()

        await vi.advanceTimersByTimeAsync(10_000)
        expect(store.dismissed.has('t_1')).toBe(false)

        store.accept(task('t_2', 1))
        await nextTick()

        wrapper.findComponent(stubs.APopover).vm.$emit('update:open', false)
        await nextTick()

        expect(store.dismissed.has('t_1')).toBe(true)
        expect(store.dismissed.has('t_2')).toBe(false)
        wrapper.unmount()
    })
})
