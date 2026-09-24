import { describe, expect, it } from 'vitest'
import { isDrawerToggle } from './shortcuts'

describe('isDrawerToggle', () => {
    it('takes M, with or without Caps Lock, and leaves modified presses and repeats alone', () => {
        const key = (init: KeyboardEventInit) => isDrawerToggle(new KeyboardEvent('keydown', init))
        expect(key({ key: 'm' })).toBe(true)
        expect(key({ key: 'M' })).toBe(true)
        expect(key({ key: 'm', ctrlKey: true })).toBe(false)
        expect(key({ key: 'M', shiftKey: true })).toBe(false)
        expect(key({ key: 'm', repeat: true })).toBe(false)
        expect(key({ key: 'n' })).toBe(false)
    })
})
