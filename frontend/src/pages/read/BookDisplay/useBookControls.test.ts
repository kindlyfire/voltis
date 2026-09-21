import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getClickZone } from '../useClickZones'
import { createTapGuard } from './useBookControls'

type TapGuard = ReturnType<typeof createTapGuard>

function selection(text: string) {
    vi.spyOn(window, 'getSelection').mockReturnValue({
        toString: () => text,
    } as unknown as Selection)
}

/** `composedPath()` is only populated while the event is being dispatched, so
 * the guard has to be asked from inside a listener, as it is in the reader. */
function tap(guard: TapGuard, target: Element, init: MouseEventInit = {}) {
    let allowed: boolean | null = null
    root.addEventListener(
        'click',
        e => {
            allowed = guard.allows(e)
            // jsdom refuses to follow the external link otherwise.
            e.preventDefault()
        },
        { once: true }
    )
    target.dispatchEvent(
        new MouseEvent('click', { bubbles: true, composed: true, cancelable: true, ...init })
    )
    return allowed
}

let root: HTMLElement
let link: HTMLElement

beforeEach(() => {
    selection('')
    // As `rewriteAnchor` leaves them: in-book links keep an href to the
    // reader, off-book ones lose it, external ones keep their own.
    document.body.innerHTML = `<div id="root">
        <a id="link" data-book-href="a.xhtml" data-book-frag="" href="/r/c_1?ch=a.xhtml">x</a>
        <a id="external" href="https://example.com" target="_blank">x</a>
        <a id="missing" data-book-missing="1">x</a>
    </div>`
    root = document.getElementById('root')!
    link = document.getElementById('link')!
})

describe('tap guard', () => {
    it('allows a plain tap', () => {
        const guard = createTapGuard()
        guard.down(new MouseEvent('pointerdown', { clientX: 10, clientY: 10 }) as PointerEvent)
        expect(tap(guard, root, { clientX: 12, clientY: 14 })).toBe(true)
    })

    it('rejects a click that ends a text selection', () => {
        selection('a selected passage')
        const guard = createTapGuard()
        expect(tap(guard, root)).toBe(false)
    })

    it('rejects a click that only dismisses a selection', () => {
        const guard = createTapGuard()
        selection('a selected passage')
        guard.down(new MouseEvent('pointerdown', { clientX: 10, clientY: 10 }) as PointerEvent)
        // The press collapses it, so the click itself sees nothing selected.
        selection('')
        expect(tap(guard, root, { clientX: 10, clientY: 10 })).toBe(false)
    })

    it('rejects a click on any kind of link', () => {
        const guard = createTapGuard()
        expect(tap(guard, link)).toBe(false)
        expect(tap(guard, document.getElementById('external')!)).toBe(false)
        expect(tap(guard, document.getElementById('missing')!)).toBe(false)
    })

    it('rejects a click the session already handled', () => {
        const guard = createTapGuard()
        root.addEventListener('click', e => e.preventDefault(), { once: true })
        expect(tap(guard, root)).toBe(false)
    })

    it('rejects a drag, then judges the next click on its own', () => {
        const guard = createTapGuard()
        guard.down(new MouseEvent('pointerdown', { clientX: 10, clientY: 10 }) as PointerEvent)
        expect(tap(guard, root, { clientX: 10, clientY: 40 })).toBe(false)
        expect(tap(guard, root, { clientX: 10, clientY: 40 })).toBe(true)
    })
})

describe('click zones', () => {
    function zone(x: number, y: number) {
        const target = {
            getBoundingClientRect: () => ({ top: 0, bottom: 800, left: 0, width: 900 }),
        }
        return getClickZone({
            currentTarget: target,
            clientX: x,
            clientY: y,
        } as unknown as MouseEvent)
    }

    it('splits the viewport the way the comic reader does', () => {
        window.innerHeight = 800
        expect(zone(450, 40)).toBe('prev')
        expect(zone(450, 760)).toBe('next')
        expect(zone(50, 400)).toBe('prev')
        expect(zone(850, 400)).toBe('next')
        expect(zone(450, 400)).toBe('menu')
    })
})
