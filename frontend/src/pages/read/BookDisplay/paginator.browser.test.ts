import { afterEach, describe, expect, it, vi } from 'vitest'
import type { BookSpread } from './bookSettings'
import { mountFixture, words, type Fixture, type FixtureDoc } from './browserFixture'
import { captureIn, locateIn, type LocatableSlice } from './locatorGeometry'
import { createPaginator, type Paginator, type ScreenState } from './paginator'

let fx: Fixture | null = null
let paginator: Paginator | null = null

afterEach(() => {
    paginator?.dispose()
    fx?.dispose()
    paginator = null
    fx = null
})

async function setup(docs: FixtureDoc[], spread: BookSpread = '1', textWidth = 20) {
    fx = await mountFixture(docs)
    fx.reader.style.setProperty('--reader-width', String(textWidth))
    const screen: ScreenState = { index: 0, count: 1, spread: 1 }
    const onUserMove = vi.fn()
    paginator = createPaginator(fx.host, screen, { spread: () => spread, onUserMove })
    paginator.layout()
    paginator.measure()
    const slices: LocatableSlice[] = fx.roots.map((root, i) => ({
        slice: { href: docs[i]!.href },
        root,
        startOffset: 0,
    }))
    return { fx, paginator, screen, onUserMove, slices }
}

function pitch(fx: Fixture) {
    const style = fx.viewport.style
    const colW = parseInt(style.getPropertyValue('--pg-col-w'))
    const gap = parseInt(style.getPropertyValue('--pg-gap'))
    return colW + gap
}

function scrollEvent(host: HTMLElement) {
    return new Promise(resolve => host.addEventListener('scroll', resolve, { once: true }))
}

const LONG: FixtureDoc = { href: 'long.xhtml', body: `<p>${words(600)}</p>` }
const SHORT: FixtureDoc = { href: 'short.xhtml', body: `<p>${words(40, 's')}</p>` }
const PLATE: FixtureDoc = {
    href: 'plate.xhtml',
    body: '<svg width="200" height="100" style="display:block"></svg>',
}

describe('paginator', () => {
    it('writes integer geometry and counts every slice from a fresh column', async () => {
        const { fx, screen } = await setup([PLATE, LONG, SHORT])
        for (const name of ['--pg-col-w', '--pg-gap', '--pg-h', '--pg-frame-w']) {
            expect(fx.viewport.style.getPropertyValue(name)).toMatch(/^\d+px$/)
        }
        const colPitch = pitch(fx)
        const lefts = fx.holders.map(holder => parseInt(holder.style.left))
        expect(lefts[0]).toBe(0)
        expect(lefts[1]).toBe(colPitch)
        expect(lefts[2]! % colPitch).toBe(0)
        expect(screen.count).toBeGreaterThan(3)
        const extender = fx.host.querySelector<HTMLElement>('.book-extender')!
        expect(parseInt(extender.style.left)).toBe(screen.count * colPitch - 1)
    })

    it.each(['1', '2'] as const)(
        'lands every screen at index × pitch (spread %s)',
        async spread => {
            const { fx, paginator, screen } = await setup([PLATE, LONG, PLATE], spread, 12)
            const screenPitch = pitch(fx) * screen.spread
            for (let i = 0; i < screen.count; i++) {
                paginator.showScreen(i)
                expect(fx.host.scrollLeft).toBe(i * screenPitch)
            }
            paginator.showScreen(screen.count + 5)
            expect(screen.index).toBe(screen.count - 1)
        }
    )

    it('finds the screen of a target in a later slice', async () => {
        const { fx, paginator, screen } = await setup([LONG, SHORT])
        const last = fx.roots[1]!.querySelector('p')!
        const index = paginator.screenOf(last)!
        expect(index).toBe(screen.count - 1)
        paginator.showScreen(index)
        const rect = last.getBoundingClientRect()
        const frame = fx.host.getBoundingClientRect()
        expect(rect.left).toBeGreaterThanOrEqual(frame.left - 1)
        expect(rect.left).toBeLessThan(frame.right)
    })

    it('snaps a foreign scroll to the nearest screen and reports it', async () => {
        const { fx, screen, onUserMove } = await setup([LONG])
        const colPitch = pitch(fx)
        const scrolled = scrollEvent(fx.host)
        fx.host.scrollLeft = 2 * colPitch + 30
        await scrolled
        expect(screen.index).toBe(2)
        expect(fx.host.scrollLeft).toBe(2 * colPitch)
        expect(onUserMove).toHaveBeenCalledOnce()
    })

    it('ignores its own scrolls', async () => {
        const { fx, paginator, onUserMove } = await setup([LONG])
        const scrolled = scrollEvent(fx.host)
        paginator.showScreen(3)
        await scrolled
        expect(onUserMove).not.toHaveBeenCalled()
    })

    it('holds still during a selection drag, then follows the selection', async () => {
        const { fx, screen, onUserMove } = await setup([LONG])
        const colPitch = pitch(fx)
        const text = fx.roots[0]!.querySelector('p')!.firstChild as Text
        window.dispatchEvent(new PointerEvent('pointerdown'))
        const selection = window.getSelection()!
        const at = text.data.indexOf('w300')
        selection.setBaseAndExtent(text, 0, text, at)
        const scrolled = scrollEvent(fx.host)
        fx.host.scrollLeft = colPitch + 50
        await scrolled
        expect(screen.index).toBe(0)
        expect(fx.host.scrollLeft).toBe(colPitch + 50)

        window.dispatchEvent(new PointerEvent('pointerup'))
        const focus = new Range()
        focus.setStart(text, at)
        focus.setEnd(text, at + 1)
        const expected = Math.floor(
            (focus.getBoundingClientRect().left -
                fx.host.getBoundingClientRect().left +
                fx.host.scrollLeft +
                0.5) /
                colPitch
        )
        expect(screen.index).toBe(expected)
        expect(onUserMove).toHaveBeenCalledOnce()
        selection.removeAllRanges()
    })
})

describe('engine behaviour the paginator relies on', () => {
    it('agrees with caretPositionFromPoint on the glyph opening each screen', async () => {
        const { fx, paginator, screen, slices } = await setup([PLATE, LONG, SHORT])
        for (let k = 1; k < screen.count; k++) {
            paginator.showScreen(k)
            const range = locateIn(slices, captureIn(slices, paginator.frame())!) as Range
            const frame = fx.host.getBoundingClientRect()
            const caret = document.caretPositionFromPoint?.(frame.left + 2, frame.top + 2, {
                shadowRoots: fx.holders.map(holder => holder.shadowRoot!),
            })
            if (!caret) continue
            expect(caret.offsetNode).toBe(range.startContainer)
            expect(Math.abs(caret.offset - range.startOffset)).toBeLessThanOrEqual(1)
        }
    })

    it('counts past overflow that scrollWidth over-counts', async () => {
        const { fx, paginator, screen } = await setup([
            { href: 'o.xhtml', body: `<p>${words(40)}</p>` },
        ])
        const before = screen.count
        const wide = document.createElement('div')
        wide.style.cssText = 'width: 900px; height: 10px'
        const link = document.createElement('p')
        link.textContent = `https://example.com/${'a'.repeat(300)}`
        fx.roots[0]!.append(wide, link)
        paginator.measure()
        expect(screen.count).toBe(before)
        expect(fx.host.scrollWidth).toBeGreaterThan(screen.count * pitch(fx))
    })

    it('resolves shadow body margins against the column and paints the body per column', async () => {
        const { fx, screen } = await setup([
            { ...LONG, css: 'body { margin: 0 2%; background: #fee }' },
        ])
        const body = fx.roots[0]!
        const colW = parseInt(fx.viewport.style.getPropertyValue('--pg-col-w'))
        expect(parseFloat(getComputedStyle(body).marginLeft)).toBeCloseTo(colW * 0.02, 0)
        expect(body.getClientRects().length).toBe(screen.count)
    })

    it('gives a display:none empty slice no column', async () => {
        const { fx, paginator } = await setup([PLATE, { href: 'e.xhtml', body: '' }, SHORT])
        fx.holders[1]!.classList.add('is-empty')
        paginator.measure()
        expect(parseInt(fx.holders[2]!.style.left)).toBe(pitch(fx))
    })

    it('clamps the screen when the count shrinks', async () => {
        const { fx, paginator, screen } = await setup([LONG])
        paginator.showScreen(screen.count - 1)
        fx.roots[0]!.querySelector('p')!.textContent = words(20)
        paginator.measure()
        expect(screen.index).toBe(0)
    })
})

describe('paged user styles', () => {
    it('outranks the book own !important rules', async () => {
        const { fx } = await setup([
            {
                href: 'img.xhtml',
                body: '<img class="big" width="200" height="5000">',
                css: 'img.big:not(#a) { max-height: none !important; max-width: none !important }',
            },
        ])
        fx.holders[0]!.classList.add('is-paged')
        const img = fx.roots[0]!.querySelector('img')!
        const pageH = parseInt(fx.viewport.style.getPropertyValue('--pg-h'))
        expect(img.getBoundingClientRect().height).toBeLessThanOrEqual(pageH + 1)
        fx.holders[0]!.classList.remove('is-paged')
        expect(img.getBoundingClientRect().height).toBeGreaterThan(pageH)
    })
})

describe('locator geometry in a paged frame', () => {
    it('captures the first glyph of screen k, across slices', async () => {
        const { fx, paginator, screen, slices } = await setup([SHORT, LONG])
        for (let k = 0; k < screen.count; k++) {
            paginator.showScreen(k)
            const locator = captureIn(slices, paginator.frame())!
            const target = locateIn(slices, locator)!
            expect(paginator.screenOf(target)).toBe(k)
            // Nothing earlier in reading order is on this screen.
            const rect = (target as Range).getBoundingClientRect()
            expect(rect.left).toBeGreaterThanOrEqual(fx.host.getBoundingClientRect().left - 1)
        }
        paginator.showScreen(1)
        expect(captureIn(slices, paginator.frame())!.href).toBe('long.xhtml')
    })

    it('steps over an off-screen positioned block while searching', async () => {
        const paragraphs = Array.from({ length: 40 }, (_, i) => `<p>${words(30, `p${i}_`)}</p>`)
        // Where the search probes first, "before" every screen.
        paragraphs.splice(20, 0, '<div style="position: absolute; left: -9999px">marker</div>')
        const { paginator, screen, slices } = await setup([
            { href: 'm.xhtml', body: paragraphs.join('') },
        ])
        for (let k = 0; k < screen.count; k++) {
            paginator.showScreen(k)
            const target = locateIn(slices, captureIn(slices, paginator.frame())!)!
            expect(paginator.screenOf(target)).toBe(k)
        }
    })

    it('captures mid-paragraph where one text node spans columns', async () => {
        const { paginator, slices } = await setup([LONG])
        paginator.showScreen(2)
        const locator = captureIn(slices, paginator.frame())!
        expect(locator.textOffset).toBeGreaterThan(0)
        const point = locateIn(slices, locator) as Range
        expect(point.startContainer).toBe(slices[0]!.root.querySelector('p')!.firstChild)
    })

    it('steps over hidden text', async () => {
        const { paginator, slices } = await setup([
            { href: 'h.xhtml', body: `<p><span hidden>secret</span>${words(300)}</p>` },
        ])
        paginator.showScreen(0)
        const locator = captureIn(slices, paginator.frame())!
        const range = locateIn(slices, locator) as Range
        expect(range.toString()).toBe('w')
    })

    it('falls back to the block start of vertical text', async () => {
        const { paginator, slices } = await setup([
            { href: 'v.xhtml', body: `<p style="writing-mode: vertical-rl">${words(20)}</p>` },
        ])
        const locator = captureIn(slices, paginator.frame())!
        expect(locator.textOffset).toBe(0)
    })

    it('is a fixed point: capture, reveal, capture', async () => {
        const { paginator, slices } = await setup([SHORT, LONG], '2', 14)
        paginator.showScreen(2)
        const first = captureIn(slices, paginator.frame())!
        paginator.showScreen(0)
        paginator.showScreen(paginator.screenOf(locateIn(slices, first)!)!)
        expect(captureIn(slices, paginator.frame())).toEqual(first)
    })

    it('keeps the passage through a font-size relayout', async () => {
        const { fx, paginator, slices } = await setup([LONG])
        paginator.showScreen(3)
        const before = captureIn(slices, paginator.frame())!
        for (const size of ['22px', '12px', '16px']) {
            fx.reader.style.setProperty('--reader-font-size', size)
            paginator.layout()
            paginator.measure()
            const target = locateIn(slices, before)!
            paginator.showScreen(paginator.screenOf(target)!)
            const rect = (target as Range).getBoundingClientRect()
            const frame = fx.host.getBoundingClientRect()
            expect(rect.left).toBeGreaterThanOrEqual(frame.left - 1)
            expect(rect.left).toBeLessThan(frame.right)
        }
        expect(locateIn(slices, before)).toBeTruthy()
    })
})
