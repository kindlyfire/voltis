import { VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { usersApi } from '@/utils/api/users'
import { queryClient } from '@/utils/misc'

vi.mock('@/utils/ws', () => ({ ws: { on: () => () => {}, connect: () => {} } }))

describe('useMe', () => {
    it('does not refetch a failed load when another consumer mounts', async () => {
        const fetch = vi.fn(async () => Response.json({ error: 'taken' }, { status: 403 }))
        vi.stubGlobal('fetch', fetch)
        let q!: ReturnType<typeof usersApi.useMe>
        const Consumer = { setup: () => void (q = usersApi.useMe()), template: '<i />' }
        const render = () =>
            mount(Consumer, { global: { plugins: [[VueQueryPlugin, { queryClient }]] } })

        render()
        await flushPromises()
        expect(q.isError.value).toBe(true)
        render()
        await flushPromises()
        expect(fetch).toHaveBeenCalledTimes(1)
        vi.unstubAllGlobals()
    })
})
