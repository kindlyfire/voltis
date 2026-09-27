import { describe, expect, it } from 'vitest'
import { classifySwipe, createSwipe } from './swipe'

const at = (x: number, y = 0, time = 0) => ({ x, y, time })

function touches(...points: Array<[number, number]>) {
    return points.map(([clientX, clientY]) => ({ clientX, clientY })) as unknown as TouchList
}

describe('classify swipe', () => {
    it('turns forward on a leftward flick and back on a rightward one', () => {
        expect(classifySwipe(at(300), at(200, 10, 200), 390)).toBe('next')
        expect(classifySwipe(at(100), at(200, -10, 200), 390)).toBe('prev')
    })

    it('needs 40px or 6% of the width, whichever is more', () => {
        expect(classifySwipe(at(300), at(265, 0, 100), 390)).toBeNull()
        expect(classifySwipe(at(1000), at(930, 0, 100), 1920)).toBeNull()
        expect(classifySwipe(at(1000), at(880, 0, 100), 1920)).toBe('next')
    })

    it('rejects mostly vertical movement and long presses', () => {
        expect(classifySwipe(at(300), at(200, 60, 200), 390)).toBeNull()
        expect(classifySwipe(at(300), at(200, 0, 900), 390)).toBeNull()
    })
})

describe('swipe gestures', () => {
    const start = (x: number, ...extra: Array<[number, number]>) => ({
        touches: touches([x, 0], ...extra),
        changedTouches: touches([x, 0]),
        timeStamp: 0,
    })
    const end = (x: number, remaining: Array<[number, number]> = []) => ({
        touches: touches(...remaining),
        changedTouches: touches([x, 0]),
        timeStamp: 150,
    })

    it('turns on a one-finger flick', () => {
        const swipe = createSwipe({ hasSelection: () => false, isZoomed: () => false })
        swipe.start(start(300))
        expect(swipe.end(end(100), 390)).toBe('next')
    })

    it('ignores a second finger, a selection and zoom', () => {
        let selected = false
        let zoomed = false
        const swipe = createSwipe({ hasSelection: () => selected, isZoomed: () => zoomed })
        swipe.start(start(300, [100, 0]))
        expect(swipe.end(end(100, [[100, 0]]), 390)).toBeNull()
        expect(swipe.end(end(100), 390)).toBeNull()

        selected = true
        swipe.start(start(300))
        expect(swipe.end(end(100), 390)).toBeNull()

        selected = false
        zoomed = true
        swipe.start(start(300))
        expect(swipe.end(end(100), 390)).toBeNull()
    })
})
