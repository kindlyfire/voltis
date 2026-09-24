import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { toasts, useToast } from './useToast'

beforeEach(() => {
    vi.useFakeTimers()
    toasts.value = []
})
afterEach(() => {
    vi.useRealTimers()
})

const messages = () => toasts.value.map(t => t.message)

describe('useToast', () => {
    it('shows one toast at a time, the next after the current is dismissed', () => {
        const { show } = useToast()
        const first = show({ message: 'first' })
        show({ message: 'second' })
        expect(messages()).toEqual(['first', 'second'])

        first.dismiss()
        expect(toasts.value[0]!.open).toBe(false)
        vi.advanceTimersByTime(200)
        expect(messages()).toEqual(['second'])
    })

    it('bounds the queue by dropping the oldest waiting toast', () => {
        const { show } = useToast()
        for (const n of [1, 2, 3, 4, 5]) show({ message: `${n}` })
        expect(messages()).toEqual(['1', '3', '4', '5'])
    })

    it('removes a waiting toast immediately when dismissed', () => {
        const { show } = useToast()
        show({ message: 'first' })
        show({ message: 'second' }).dismiss()
        expect(messages()).toEqual(['first'])
    })
})
