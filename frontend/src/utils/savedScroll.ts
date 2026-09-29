import type { Router } from 'vue-router'

// The window's scroll top when each history entry was last left, or the target of a restore
// still in progress then. Mirrors the router's private saved positions, which record `scrollY`
// even mid-restore. Keyed like the router's: the entry's position plus its path.
const positions = new Map<string, number>()
let current: string | undefined

const entryKey = () => (history.state ? `${history.state.position}${history.state.current}` : '')

/** `pendingTop` is the target of a restore still in progress, which `scrollY` hasn't reached. */
export function trackSavedScroll(router: Router, pendingTop: () => number | null) {
    // On Back/Forward `history.state` already belongs to the destination, so the entry being
    // left is the one recorded after the last navigation.
    router.beforeEach(() => {
        if (current !== undefined) positions.set(current, pendingTop() ?? window.scrollY)
    })
    router.afterEach(() => {
        current = entryKey()
        // A new entry may reuse a forgotten one's key. vue-router pushes it with `scroll: null`,
        // and a `replace` sets `scroll: false`.
        if (!history.state?.scroll) positions.delete(current)
    })
}

/** The mirrored scroll top of the current entry, if it has one. */
export function savedTop(): number | undefined {
    return positions.get(entryKey())
}
