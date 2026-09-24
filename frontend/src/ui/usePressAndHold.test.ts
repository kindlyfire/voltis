import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope } from 'vue'
import { usePressAndHold } from './usePressAndHold'

let fn: ReturnType<typeof vi.fn<() => void>>
let handlers: ReturnType<typeof usePressAndHold>
const scope = effectScope()

beforeEach(() => {
    vi.useFakeTimers()
    fn = vi.fn<() => void>()
    handlers = scope.run(() => usePressAndHold(fn, { initialDelay: 400, interval: 100 }))!
})
afterEach(() => {
    handlers.onPointerup()
    vi.useRealTimers()
})

const pointer = (type: string) => new PointerEvent(type, { button: 0 })
const key = (type: string, init: KeyboardEventInit = {}) =>
    new KeyboardEvent(type, { key: 'Enter', cancelable: true, ...init })

describe('usePressAndHold', () => {
    it('runs once for a short press, including its click', () => {
        handlers.onPointerdown(pointer('pointerdown'))
        handlers.onPointerup()
        handlers.onClick(new MouseEvent('click', { detail: 1 }))
        vi.advanceTimersByTime(1000)
        expect(fn).toHaveBeenCalledTimes(1)
    })

    it('repeats while held and stops on cancel', () => {
        handlers.onPointerdown(pointer('pointerdown'))
        vi.advanceTimersByTime(600)
        expect(fn).toHaveBeenCalledTimes(4)
        handlers.onPointercancel()
        vi.advanceTimersByTime(1000)
        expect(fn).toHaveBeenCalledTimes(4)
    })

    it('repeats while Enter is held, without the native click', () => {
        const down = key('keydown')
        handlers.onKeydown(down)
        handlers.onKeydown(key('keydown', { repeat: true }))
        expect(down.defaultPrevented).toBe(true)
        vi.advanceTimersByTime(400)
        handlers.onKeyup(key('keyup'))
        vi.advanceTimersByTime(1000)
        expect(fn).toHaveBeenCalledTimes(2)
    })

    it('runs once for a synthetic click (assistive tech)', () => {
        handlers.onClick(new MouseEvent('click', { detail: 0 }))
        expect(fn).toHaveBeenCalledTimes(1)
    })
})
