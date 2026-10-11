import { flushPromises } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import { contentApi, EMPTY_USER_DATA } from '@/utils/api/content'
import type { Content, ReaderSettings, UserToContent } from '@/utils/api/types'
import { queryClient } from '@/utils/misc'

const key = ['content', 's_1']

afterEach(() => {
    queryClient.clear()
    vi.unstubAllGlobals()
})

/** Each request waits for the test to answer it, with the row's settings or null for a failure. */
function stubServer() {
    const requests: { body: unknown; answer(rs: ReaderSettings | null): void }[] = []
    vi.stubGlobal(
        'fetch',
        vi.fn(
            (_url: string, init: RequestInit) =>
                new Promise<Response>(resolve =>
                    requests.push({
                        body: JSON.parse(init.body as string),
                        answer: rs =>
                            resolve(
                                rs
                                    ? Response.json({ ...EMPTY_USER_DATA, reader_settings: rs })
                                    : Response.json({ error: 'down' }, { status: 500 })
                            ),
                    })
                )
        )
    )
    return requests
}

const seed = (user_data: UserToContent | null) =>
    queryClient.setQueryData(key, { id: 's_1', user_data } as Content)
const cached = () => queryClient.getQueryData<Content>(key)!.user_data
const rtl = { ...EMPTY_USER_DATA, rating: 7, reader_settings: { direction: 'rtl' as const } }

it('shows a save at once, then the server value, or the previous one after a failure', async () => {
    const requests = stubServer()
    seed(rtl)
    const saved = contentApi.saveReaderSettings('s_1', { mode: 'longstrip' })
    expect(cached()?.reader_settings).toEqual({ direction: 'rtl', mode: 'longstrip' })
    await flushPromises()
    expect(requests[0]!.body).toEqual({ reader_settings: { mode: 'longstrip' } })
    seed(rtl) // A stale refetch lands meanwhile; the success still shows.
    requests[0]!.answer({ mode: 'longstrip', direction: 'rtl', shift: true })
    await saved
    expect(cached()).toMatchObject({
        rating: 7,
        reader_settings: { mode: 'longstrip', direction: 'rtl', shift: true },
    })

    seed(rtl)
    const failed = contentApi.saveReaderSettings('s_1', { direction: null, mode: 'paged' })
    expect(cached()?.reader_settings).toEqual({ mode: 'paged' })
    await flushPromises()
    requests[1]!.answer(null)
    await expect(failed).rejects.toThrow()
    expect(cached()?.reader_settings).toEqual({ direction: 'rtl' })

    // Replaced while saving: the failure leaves the new value.
    const replaced = contentApi.saveReaderSettings('s_1', { mode: 'paged' })
    seed({ ...rtl, reader_settings: { shift: true } })
    await flushPromises()
    requests[2]!.answer(null)
    await expect(replaced).rejects.toThrow()
    expect(cached()?.reader_settings).toEqual({ shift: true })

    seed(null)
    const created = contentApi.saveReaderSettings('s_1', { shift: true })
    expect(cached()).toEqual({ ...EMPTY_USER_DATA, reader_settings: { shift: true } })
    await flushPromises()
    requests[3]!.answer({ shift: true })
    await created

    // A save queued behind another is dropped once the account changes, and neither result shows.
    queryClient.setQueryData(['users', 'me'], { id: 'u_1' })
    const first = contentApi.saveReaderSettings('s_1', { mode: 'paged' })
    const queued = contentApi.saveReaderSettings('s_1', { mode: 'longstrip' })
    await flushPromises()
    queryClient.setQueryData(['users', 'me'], { id: 'u_2' })
    requests[4]!.answer({ mode: 'paged' })
    await first
    await expect(queued).rejects.toThrow('someone else')
    expect(requests).toHaveLength(5)
    expect(cached()?.reader_settings).toEqual({ shift: true, mode: 'longstrip' })
})

it.each<[string, ReaderSettings | null, ReaderSettings | null, ReaderSettings]>([
    [
        'A fails, B succeeds',
        null,
        { mode: 'longstrip', shift: true },
        { mode: 'longstrip', shift: true },
    ],
    ['both fail', null, null, { direction: 'rtl' }],
    ['A succeeds, B fails', { mode: 'longstrip' }, null, { mode: 'longstrip' }],
])('two saves in a row: %s', async (_name, answerA, answerB, want) => {
    const requests = stubServer()
    seed(rtl)
    const a = contentApi.saveReaderSettings('s_1', { mode: 'longstrip' }).catch(() => {})
    const b = contentApi.saveReaderSettings('s_1', { shift: true }).catch(() => {})
    await flushPromises()
    expect(requests).toHaveLength(1)

    requests[0]!.answer(answerA)
    await a
    await flushPromises()
    expect(requests).toHaveLength(2)
    expect(requests[1]!.body).toEqual({ reader_settings: { shift: true } })
    expect(cached()?.reader_settings).toEqual({ direction: 'rtl', mode: 'longstrip', shift: true })

    requests[1]!.answer(answerB)
    await b
    expect(cached()?.reader_settings).toEqual(want)
})
