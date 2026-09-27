import { afterEach, describe, expect, it, vi } from 'vitest'
import { mountFixture, words, type Fixture } from './browserFixture'
import { createPagedLayout, createScrollLayout, type ReadingLayout } from './readingLayout'

const fixtures: Fixture[] = []
let layout: ReadingLayout | null = null

afterEach(() => {
    vi.useRealTimers()
    layout?.dispose()
    layout = null
    for (const fx of fixtures.splice(0)) fx.dispose()
    window.scrollTo({ top: 0, behavior: 'instant' })
})

const frame = () => new Promise(resolve => requestAnimationFrame(resolve))
const frames = async () => {
    await frame()
    await frame()
}
const wait = (ms: number) => new Promise(resolve => setTimeout(resolve, ms))

function slicesOf(fx: Fixture) {
    return fx.roots.map(root => ({ slice: { href: 'a.xhtml' }, root, startOffset: 0 }))
}

async function setup() {
    const fx = await mountFixture([{ href: 'a.xhtml', body: `<p>${words(400)}</p>` }])
    fixtures.push(fx)
    // A web font finishing (the test page's own) would relayout too.
    await document.fonts.ready
    await frame()
    layout = createPagedLayout({ seed: null, spread: () => '1', onUserMove: () => {} })
    layout.attach(fx.host, slicesOf(fx))
    // Each relayout writes the geometry once.
    const setProperty = vi.spyOn(fx.viewport.style, 'setProperty')
    const relayouts = () => setProperty.mock.calls.filter(([name]) => name === '--pg-col-w').length
    return { fx, layout, relayouts }
}

const extenders = (fx: Fixture) => fx.host.querySelectorAll('.book-extender').length

describe('paged layout', () => {
    it('coalesces relayouts to one per frame', async () => {
        const { layout, relayouts } = await setup()
        for (let i = 0; i < 5; i++) layout.reflow()
        await frame()
        await frame()
        expect(relayouts()).toBe(1)
    })

    it('relayouts once after a burst of resizes, not on first observing', async () => {
        // Frames stay real (resizes are observed during rendering); the debounce
        // runs on fake time, so a slow frame can't split the burst.
        vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
        const { fx, relayouts } = await setup()
        await frames()
        vi.advanceTimersByTime(200)
        await frames()
        expect(relayouts()).toBe(0)
        for (const width of ['900px', '800px', '700px']) {
            fx.reader.style.width = width
            await frames()
            vi.advanceTimersByTime(60)
        }
        await frames()
        // Each resize restarted the wait.
        expect(relayouts()).toBe(0)
        vi.advanceTimersByTime(40)
        await frames()
        expect(relayouts()).toBe(1)
    })

    it('moves to a new host', async () => {
        const { fx, layout } = await setup()
        const next = await mountFixture([{ href: 'b.xhtml', body: `<p>${words(50)}</p>` }])
        fixtures.push(next)
        layout.attach(next.host, slicesOf(next))
        expect(extenders(fx)).toBe(0)
        expect(extenders(next)).toBe(1)
    })

    it('does nothing once disposed', async () => {
        const { fx, layout, relayouts } = await setup()
        layout.dispose()
        fx.holders[0]!.shadowRoot!.querySelector('p')!.dispatchEvent(new Event('load'))
        document.fonts.dispatchEvent(new Event('loadingdone'))
        fx.reader.style.width = '600px'
        layout.reflow()
        layout.attach(fx.host, slicesOf(fx))
        await wait(150)
        await frame()
        expect(relayouts()).toBe(0)
        expect(extenders(fx)).toBe(0)
    })
})

describe('scroll layout', () => {
    it('keeps a placement made before binding provisional', async () => {
        const fx = await mountFixture([{ href: 'a.xhtml', body: `<p>${words(3000)}</p>` }])
        fixtures.push(fx)
        fx.reader.classList.remove('is-paged')
        layout = createScrollLayout({ seed: null, spread: () => '1', onUserMove: () => {} })
        const far = {
            anchor: null,
            locator: { version: 1 as const, href: 'a.xhtml', textOffset: 10000 },
        }
        expect(layout.place(far, true)).toBe('pending')
        layout.attach(fx.host, slicesOf(fx))
        expect(window.scrollY).toBe(0)
        expect(layout.place(far)).toBe(true)
        expect(window.scrollY).toBeGreaterThan(0)
    })
})
