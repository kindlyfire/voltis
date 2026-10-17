import { VueQueryPlugin } from '@tanstack/vue-query'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import ScrobbleModal from '@/pages/content/components/ScrobbleModal.vue'
import { apiFetch } from '@/utils/fetch'
import { queryClient } from '@/utils/misc'

vi.mock('@/utils/fetch', () => ({ apiFetch: vi.fn() }))
vi.mock('@/utils/api/users', () => ({ usersApi: { useMe: () => ({ data: ref({ id: 'u1' }) }) } }))
vi.mock('@/stores/scrobbles', () => ({
    useScrobbleStore: () => ({ loaded: false, row: () => undefined }),
}))

const push: (() => void)[] = []
vi.mock('@/utils/ws', () => ({
    ws: {
        on: (type: string, fn: () => void) => {
            if (type === 'scrobbles_changed') push.push(fn)
            return () => {}
        },
    },
}))

const content = (state?: 'pending', remoteChapter = 1) => ({
    available: 'ok',
    held: false,
    state,
    ours: { chapter: 3 },
    ahead: [],
    linked: [],
    remote: { chapter: remoteChapter },
})

let gets: ((v: unknown) => void)[]
let rejected: boolean
let wrapper: ReturnType<typeof mount> | undefined

beforeEach(() => {
    vi.useFakeTimers()
    vi.mocked(apiFetch).mockImplementation((url: string) => {
        if (url.endsWith('/users/me/scrobbling'))
            return Promise.resolve({ mangabaka: { rejected } })
        if (url.endsWith('/scrobble/send')) return Promise.resolve({ ok: true })
        return new Promise(resolve => gets.push(resolve))
    })
})

afterEach(() => {
    wrapper?.unmount()
    queryClient.clear()
    vi.useRealTimers()
})

const settle = () => vi.advanceTimersByTimeAsync(10)

// Opens the dialog on a pending series and clicks "Send now"; returns its loading state.
async function clickSend(tokenRejected = false) {
    rejected = tokenRejected
    wrapper?.unmount()
    queryClient.clear()
    gets = []
    push.length = 0
    wrapper = mount(ScrobbleModal, {
        props: { open: true, close: () => {}, contentId: 'c1' },
        global: {
            plugins: [[VueQueryPlugin, { queryClient }]],
            stubs: { ADialog: { template: '<div><slot /><slot name="actions" /></div>' } },
        },
    })
    await settle()
    gets.shift()!(content('pending'))
    await settle()
    const button = wrapper.findAll('button').find(b => b.text() === 'Send now')!
    await button.trigger('click')
    await settle()
    return () => button.classes().includes('loading')
}

describe('ScrobbleModal send', () => {
    it('keeps loading until a fetch after the send shows the series is no longer pending', async () => {
        // A push during the fetch queues a follow-up, which must settle too, and a response that
        // still shows pending keeps waiting until a terminal one lands.
        let loading = await clickSend()
        expect(loading()).toBe(true)
        push[0]()
        gets.shift()!(content())
        await settle()
        expect(loading()).toBe(true)
        gets.shift()!(content('pending'))
        await settle()
        expect(loading()).toBe(true)
        push[0]()
        await settle()
        gets.shift()!(content(undefined, 7))
        await settle()
        expect(loading()).toBe(false)
        expect(wrapper!.text()).toContain('7')

        // A rejected token stops it while the refetch is still out.
        loading = await clickSend(true)
        expect(loading()).toBe(false)

        // A stuck fetch gives up after 30s.
        loading = await clickSend()
        expect(loading()).toBe(true)
        await vi.advanceTimersByTimeAsync(30_000)
        expect(loading()).toBe(false)
    })
})
