import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { pendingRestoreTop, useRestoreReady, whenReachable } from './whenReachable'

enableAutoUnmount(afterEach)

const pos = { left: 0, top: 1000 }
let height: number
let ready: boolean
let current: boolean
const abort = vi.fn()

beforeEach(() => {
    vi.useFakeTimers({
        toFake: [
            'setTimeout',
            'clearTimeout',
            'requestAnimationFrame',
            'cancelAnimationFrame',
            'performance',
        ],
    })
    vi.stubGlobal('innerHeight', 800)
    Object.defineProperty(document.documentElement, 'scrollHeight', {
        configurable: true,
        get: () => height,
    })
    abort.mockClear()
    mount(defineComponent({ setup: () => useRestoreReady(() => ready, abort), render: () => null }))
})

afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    // The own property shadows jsdom's prototype getter.
    delete (document.documentElement as { scrollHeight?: number }).scrollHeight
})

const wheel = () => window.dispatchEvent(new WheelEvent('wheel', { deltaY: 10 }))

it.each([
    { name: 'reachable', height: 1800, result: pos },
    {
        name: 'not current',
        height: 0,
        act: () => {
            current = false
            expect(pendingRestoreTop()).toBe(null)
        },
        result: false,
    },
    {
        name: 'wheel in the grace period, then after it',
        height: 0,
        act: async () => {
            wheel()
            await vi.advanceTimersByTimeAsync(50)
            expect(pendingRestoreTop()).toBe(1000)
            await vi.advanceTimersByTimeAsync(100)
            wheel()
        },
        result: false,
    },
    {
        name: 'a not-ready registrant, until the deadline',
        height: 1800,
        ready: false,
        act: async () => {
            await vi.advanceTimersByTimeAsync(2900)
            expect(pendingRestoreTop()).toBe(1000)
            await vi.advanceTimersByTimeAsync(100)
        },
        result: false,
    },
    {
        name: 'a second call',
        height: 0,
        act: async () => {
            const second = whenReachable({ left: 0, top: 500 }, () => true)
            await vi.advanceTimersByTimeAsync(0)
            expect(pendingRestoreTop()).toBe(500)
            height = 1300
            await vi.advanceTimersByTimeAsync(20)
            expect(await second).toEqual({ left: 0, top: 500 })
        },
        result: false,
    },
])('$name', async c => {
    height = c.height
    ready = c.ready ?? true
    current = true
    let result: unknown = 'pending'
    void whenReachable(pos, () => current).then(r => (result = r))
    await c.act?.()
    await vi.advanceTimersByTimeAsync(0)
    expect(result).toEqual(c.result)
    expect(pendingRestoreTop()).toBe(null)
    expect(abort).toHaveBeenCalledTimes(c.result ? 0 : 1)
})
