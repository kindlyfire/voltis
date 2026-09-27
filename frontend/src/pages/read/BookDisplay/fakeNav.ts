import { createMemoryHistory, createRouter } from 'vue-router'
import type { BookLocator } from '@/utils/api/types'
import type { BookAnchor, BookNav, BookSession } from './createBookSession'

/** For the session tests: a router and a history stack, so Back/Forward and
 * canonicalization are exercised the way the store wires them. */
export class FakeNav implements BookNav {
    entries: Array<{ target: BookAnchor; locator: BookLocator | null }> = []
    index = -1
    session: BookSession | null = null
    leaveGuard: (() => void) | null = null

    beforeLeave(callback: () => void) {
        this.leaveGuard = callback
        return () => {
            this.leaveGuard = null
        }
    }

    /** A push of the current entry changes nothing, as in vue-router. */
    push(target: BookAnchor) {
        const current = this.entries[this.index]?.target
        if (current?.href === target.href && current.fragment === target.fragment) {
            return duplicate()
        }
        this.leaveGuard?.()
        this.entries.splice(this.index + 1)
        this.entries.push({ target, locator: null })
        this.index++
        this.deliver()
        return Promise.resolve()
    }

    replace(target: BookAnchor) {
        if (this.index < 0) {
            this.entries.push({ target, locator: null })
            this.index = 0
        } else {
            this.entries[this.index]!.target = target
        }
        this.deliver()
    }

    /** Cloned as `history.replaceState` does, which rejects a reactive proxy. */
    saveLocator(locator: BookLocator) {
        if (this.index < 0) return
        this.entries[this.index]!.locator = structuredClone(locator)
    }

    historyLocator(): BookLocator | null {
        return this.entries[this.index]?.locator ?? null
    }

    go(delta: number) {
        const next = this.index + delta
        if (next < 0 || next >= this.entries.length) return
        this.index = next
        this.deliver()
    }

    private deliver() {
        const target = this.entries[this.index]!.target
        this.session?.setEntry({ ch: target.href, frag: target.fragment || null })
    }
}

async function duplicate() {
    const router = createRouter({
        history: createMemoryHistory(),
        routes: [{ path: '/', component: {} }],
    })
    await router.push('/')
    return router.push('/')
}
