import { mount } from '@vue/test-utils'
import { createPinia, getActivePinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick, ref } from 'vue'
import { createComicState } from './createComicState'
import { detectMode, pageStyle, useReaderStore } from './useComicDisplayStore'
import { useReaderControls } from './useReaderControls'

const router = vi.hoisted(() => ({ push: vi.fn(), replace: vi.fn() }))
const siblings = vi.hoisted(() => ({
    data: null as unknown as { value: { data: { id: string }[] } | undefined },
    isPlaceholderData: null as unknown as { value: boolean },
    isError: null as unknown as { value: boolean },
    isFetching: null as unknown as { value: boolean },
}))
vi.mock('vue-router', () => ({ useRouter: () => router }))
vi.mock('@/utils/api/content', () => ({
    contentApi: {
        useList: () => {
            siblings.data = ref({ data: [{ id: 'c_1' }, { id: 'c_2' }] })
            siblings.isPlaceholderData = ref(false)
            siblings.isError = ref(false)
            siblings.isFetching = ref(false)
            return { ...siblings, refetch: vi.fn() }
        },
        useGet: () => ({ data: ref(undefined) }),
    },
}))
vi.mock('./createComicState', () => ({
    createComicState: vi.fn((contentId: string) => ({
        contentId,
        content: { id: contentId, parent_id: 's_1' },
        page: 0,
        pageDimensions: [],
        setHandlers: vi.fn(),
        setPage: vi.fn(),
        sync: { flush: vi.fn() },
        dispose: vi.fn(),
    })),
}))

beforeEach(() => {
    setActivePinia(createPinia())
})

it('starts a new content disarmed and restoring, disposing the old one', () => {
    const reader = useReaderStore()
    reader.setContent({ contentId: 'c_1', initialPage: 'resume' })
    reader.armed = true
    reader.atEnd = true
    reader.restoring = false

    reader.setContent({ contentId: 'c_2', initialPage: 'resume' })
    expect(reader.armed).toBe(false)
    expect(reader.atEnd).toBe(false)
    expect(reader.restoring).toBe(true)
    const old = vi.mocked(createComicState).mock.results[0]!.value
    expect(old.dispose).toHaveBeenCalled()
})

it("doesn't go by another series' list kept while this one's loads", () => {
    const reader = useReaderStore()
    siblings.data.value = { data: [{ id: 'x_1' }, { id: 'x_2' }] }
    siblings.isPlaceholderData.value = true
    reader.setContent({ contentId: 'c_1', initialPage: 'resume' })
    expect(reader.siblings.status).toBe('loading')
    reader.goToSibling('next')
    expect(router.push).not.toHaveBeenCalled()
})

it("offers Retry past the end when this series' list no longer holds the volume", () => {
    const reader = useReaderStore()
    siblings.data.value = { data: [{ id: 'c_8' }] }
    reader.setContent({ contentId: 'c_1', initialPage: 'resume' })
    reader.goPastEnd()
    expect(reader.siblings.status).toBe('error')
    expect(reader.atEnd).toBe(true)
})

it('opens siblings at their saved page, both ways', () => {
    const reader = useReaderStore()
    reader.setContent({ contentId: 'c_1', initialPage: 'resume' })
    expect(reader.siblings.next?.id).toBe('c_2')
    reader.goToSibling('next')
    reader.setContent({ contentId: 'c_2', initialPage: 'resume' })
    reader.goToSibling('prev')
    expect(router.push.mock.calls.map(([to]) => to)).toEqual([
        { name: 'read-content', params: { id: 'c_2' }, query: { page: 'resume' } },
        { name: 'read-content', params: { id: 'c_1' }, query: { page: 'resume' } },
    ])
})

it('ends a longstrip placement once scrolling pauses, without scrollend', async () => {
    vi.useFakeTimers()
    // As in Safari before 26.2.
    let owner: object | null = window
    while (owner && !Object.hasOwn(owner, 'onscrollend')) owner = Object.getPrototypeOf(owner)
    const descriptor = owner && Object.getOwnPropertyDescriptor(owner, 'onscrollend')
    if (owner) delete (owner as Record<string, unknown>).onscrollend
    expect('onscrollend' in window).toBe(false)
    window.scrollTo = vi.fn() as never
    const page = document.createElement('div')
    page.id = 'longstrip-page-2'
    document.body.append(page)
    const reader = useReaderStore()
    reader.setContent({ contentId: 'c_1', initialPage: 'resume' })
    reader.setMode('longstrip')
    reader.goToPage(2)
    expect(reader.restoring).toBe(true)
    await vi.advanceTimersByTimeAsync(100)
    window.dispatchEvent(new Event('scroll'))
    await vi.advanceTimersByTimeAsync(100)
    expect(reader.restoring).toBe(true)
    await vi.advanceTimersByTimeAsync(60)
    expect(reader.restoring).toBe(false)
    page.remove()
    if (owner && descriptor) Object.defineProperty(owner, 'onscrollend', descriptor)
    vi.useRealTimers()
})

describe('past the end', () => {
    let controls: { unmount(): void } | null = null
    afterEach(() => controls?.unmount())

    // The reader on the last of three pages, with key controls, the siblings as `pending` leaves them.
    function atLastPage(pending: () => void) {
        const reader = useReaderStore()
        pending()
        router.push.mockClear()
        reader.setContent({ contentId: 'c_2', initialPage: 'resume' })
        Object.assign(reader.state!, {
            page: 2,
            pageDimensions: [1, 2, 3].map(() => ({ width: 1, height: 1 })),
            finish: vi.fn(),
        })
        reader.setMode('paged')
        controls = mount(
            defineComponent({
                setup: () => (useReaderControls(), () => null),
            }),
            { global: { plugins: [getActivePinia()!] } }
        )
        return reader
    }
    const press = (key: string) => window.dispatchEvent(new KeyboardEvent('keydown', { key }))
    const arrive = async () => {
        siblings.isError.value = false
        siblings.isPlaceholderData.value = false
        siblings.data.value = {
            data: [{ id: 'c_1' }, { id: 'c_2' }, { id: 'c_3' }],
        }
        await nextTick()
    }

    it.each([
        [
            'load',
            () => (
                (siblings.data.value = { data: [{ id: 'x_1' }] }),
                (siblings.isPlaceholderData.value = true)
            ),
        ],
        ['fail', () => ((siblings.data.value = undefined), (siblings.isError.value = true))],
    ])('waits on the end card while they %s; the next press goes on', async (_name, pending) => {
        const reader = atLastPage(pending)
        press('ArrowRight')
        expect(reader.state!.finish).toHaveBeenCalled()
        expect(reader.atEnd).toBe(true)
        await arrive()
        expect(router.push).not.toHaveBeenCalled()
        press('ArrowRight')
        expect(router.push).toHaveBeenCalledWith(expect.objectContaining({ params: { id: 'c_3' } }))
    })

    it('ignores input until the saved page is placed', () => {
        const reader = atLastPage(() => {})
        Object.assign(reader.state!, { loading: true })
        press('ArrowRight')
        press('ArrowLeft')
        Object.assign(reader.state!, { loading: false, error: 'Failed' })
        press('ArrowLeft')
        expect(reader.state!.setPage).not.toHaveBeenCalled()
        expect(reader.state!.finish).not.toHaveBeenCalled()
    })

    it('shows the end card past the last volume', async () => {
        const reader = atLastPage(() => {})
        siblings.data.value = { data: [{ id: 'c_1' }, { id: 'c_2' }] }
        await nextTick()
        press('ArrowRight')
        expect(reader.siblings).toMatchObject({ status: 'ready', next: null })
        expect(reader.atEnd).toBe(true)
        expect(router.push).not.toHaveBeenCalled()
    })
})

it.each([
    ['paged', 'paged' as const],
    ['Auto', null],
])('sends earlier reading and disarms before a switch to %s', (_name, mode) => {
    const reader = useReaderStore()
    reader.setContent({ contentId: 'c_1', initialPage: 'resume' })
    reader.setMode('longstrip')
    reader.armed = true
    const flush = vi.mocked(reader.state!.sync.flush)
    flush.mockClear()
    reader.setMode(mode)
    expect(flush).toHaveBeenCalledOnce()
    expect(reader.armed).toBe(false)
    expect(reader.seriesSettings?.mode ?? null).toBe(mode)
})

it('parses stored settings field by field and keeps a direction across Auto mode', () => {
    localStorage.removeItem('reader:comics')
    expect(useReaderStore().settings).toEqual({
        longstripWidth: 100,
        fit: 'screen',
        spread: 'auto',
        zoomWide: true,
        invertRtlControls: true,
        seriesSettings: {},
        shiftedBooks: {},
    })

    setActivePinia(createPinia())
    localStorage.setItem(
        'reader:comics',
        JSON.stringify({
            longstripWidth: 50,
            fit: 'sideways',
            seriesSettings: { s_1: { mode: 'paged' } },
        })
    )
    const reader = useReaderStore()
    localStorage.removeItem('reader:comics')
    expect(reader.settings).toMatchObject({
        longstripWidth: 50,
        fit: 'screen',
        spread: 'auto',
        invertRtlControls: true,
        seriesSettings: { s_1: { mode: 'paged', direction: null } },
        shiftedBooks: {},
    })

    reader.setContent({ contentId: 'c_1', initialPage: 'resume' })
    reader.setDirection('rtl')
    reader.setMode(null)
    expect(reader.settings.seriesSettings).toEqual({ s_1: { mode: null, direction: 'rtl' } })
    expect(reader.direction).toBe('rtl')
    reader.setDirection(null)
    expect(reader.settings.seriesSettings).toEqual({})
})

describe('pageStyle', () => {
    it('is empty for an unknown size', () => {
        expect(pageStyle({ width: 0, height: 1200 }, 80)).toEqual({})
        expect(pageStyle({ width: 800, height: 0 }, 80)).toEqual({})
    })

    it('sizes a known page', () => {
        expect(pageStyle({ width: 800, height: 1200 }, 80)).toEqual({
            width: 'min(80%, 800px)',
            aspectRatio: '800 / 1200',
        })
    })
})

describe('detectMode', () => {
    it('is paged without known sizes', () => {
        expect(detectMode([])).toBe('paged')
        expect(detectMode([{ width: 0, height: 0 }])).toBe('paged')
    })

    it('averages only known sizes', () => {
        const strip = { width: 800, height: 3000 }
        expect(detectMode([strip, { width: 0, height: 0 }])).toBe('longstrip')
        expect(detectMode([strip, { width: 800, height: 1000 }])).toBe('longstrip')
        expect(
            detectMode([
                { width: 800, height: 1200 },
                { width: 0, height: 0 },
            ])
        ).toBe('paged')
    })
})
