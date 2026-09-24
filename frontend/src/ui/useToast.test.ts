import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { toasts, useToast, visibleToasts } from './useToast'

beforeEach(() => {
    vi.useFakeTimers()
    toasts.value = []
})
afterEach(() => {
    vi.useRealTimers()
})

const messages = () => toasts.value.map(t => t.message)
const visible = () => visibleToasts.value.map(t => t.message)

describe('useToast', () => {
    it('shows up to three toasts, the next waiting one after one is dismissed', () => {
        const { show } = useToast()
        const [, second] = [1, 2, 3, 4].map(n => show({ message: `${n}` }))
        expect(visible()).toEqual(['1', '2', '3'])

        second!.dismiss()
        expect(toasts.value[1]!.open).toBe(false)
        expect(visible()).toEqual(['1', '2', '3'])
        vi.advanceTimersByTime(200)
        expect(visible()).toEqual(['1', '3', '4'])
    })

    it('bounds the waiting toasts by dropping the oldest waiting one', () => {
        const { show } = useToast()
        for (const n of [1, 2, 3, 4, 5, 6, 7]) show({ message: `${n}` })
        expect(messages()).toEqual(['1', '2', '3', '5', '6', '7'])
    })

    it('removes a waiting toast immediately when dismissed', () => {
        const { show } = useToast()
        for (const n of [1, 2, 3]) show({ message: `${n}` })
        show({ message: 'waiting' }).dismiss()
        expect(messages()).toEqual(['1', '2', '3'])
    })
})
