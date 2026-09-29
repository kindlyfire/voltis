import { nextTick, onBeforeUnmount } from 'vue'

type Position = { left: number; top: number }
type Registrant = { ready: (target: number) => boolean; abort: () => void }

const registrants = new Set<Registrant>()
let active: { pos: Position; isCurrent: () => boolean; cancel: () => void } | null = null

// Counted from when App.vue's spinner is gone.
const TIMEOUT = 3000
// Trackpad momentum from the previous page.
const WHEEL_GRACE = 100
const SCROLL_KEYS = new Set(['PageUp', 'PageDown', 'Home', 'End', ' ', 'ArrowUp', 'ArrowDown'])

/** The restore target while a restore for the current route is pending, else `null`. */
export function pendingRestoreTop(): number | null {
    return active?.isCurrent() ? active.pos.top : null
}

/** Holds a pending restore until `ready(target)` is true. `abort` runs when a restore is given
 * up, so the component can resync with the unmoved window. */
export function useRestoreReady(ready: Registrant['ready'], abort: Registrant['abort']) {
    const r = { ready, abort }
    registrants.add(r)
    onBeforeUnmount(() => registrants.delete(r))
}

/** Resolves `pos` once every registrant is ready and the page is tall enough to reach it, or
 * `false` (no scroll) when the user scrolls, navigates or starts another restore meanwhile, or
 * after 3s. The first check runs synchronously, so a cache hit restores before the first paint. */
export function whenReachable(pos: Position, isCurrent: () => boolean) {
    active?.cancel()
    return new Promise<Position | false>(resolve => {
        const start = performance.now()
        let settled = false
        let frame = 0
        let timer: ReturnType<typeof setTimeout> | undefined

        const token = { pos, isCurrent, cancel: () => settle(false) }
        active = token

        const controller = new AbortController()
        const settle = (result: Position | false) => {
            if (settled) return
            settled = true
            cancelAnimationFrame(frame)
            clearTimeout(timer)
            controller.abort()
            if (active === token) active = null
            if (!result) for (const r of registrants) r.abort()
            resolve(result)
        }

        const options = { capture: true, passive: true, signal: controller.signal }
        window.addEventListener(
            'wheel',
            e => {
                if (performance.now() - start < WHEEL_GRACE) return
                if ((e.target as Element | null)?.closest?.('#sidebar')) return
                // Sideways, as in an AScrollRow.
                if (Math.abs(e.deltaX) > Math.abs(e.deltaY)) return
                settle(false)
            },
            options
        )
        window.addEventListener('touchstart', () => settle(false), options)
        window.addEventListener(
            'keydown',
            e => {
                const editable = (e.target as Element | null)?.closest?.(
                    'input, textarea, select, [contenteditable]'
                )
                if (SCROLL_KEYS.has(e.key) && !editable) settle(false)
            },
            options
        )
        window.addEventListener(
            'mousedown',
            e => {
                // Middle-click autoscroll, or a drag of the root scrollbar.
                if (e.button === 1 || e.target === document.documentElement) settle(false)
            },
            options
        )

        // Every registrant is asked, so each can fix itself in the same tick.
        const check = () => {
            let ready = true
            for (const r of registrants) if (!r.ready(pos.top)) ready = false
            return ready && document.documentElement.scrollHeight - window.innerHeight >= pos.top
        }

        const tick = async () => {
            if (settled) return
            if (!isCurrent()) return settle(false)
            if (timer === undefined && !document.getElementById('app-loading')) {
                timer = setTimeout(() => settle(false), TIMEOUT)
            }
            if (check()) return settle(pos)
            // A registrant that re-rendered to fix itself passes here, before the paint.
            await nextTick()
            if (settled) return
            if (!isCurrent()) return settle(false)
            if (check()) return settle(pos)
            frame = requestAnimationFrame(tick)
        }
        void tick()
    })
}
