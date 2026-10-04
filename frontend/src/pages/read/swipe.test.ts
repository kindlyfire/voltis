import { describe, expect, it } from 'vitest'
import { classifySwipe, createSwipe } from './swipe'

const at = (x: number, y = 0, time = 0) => ({ x, y, time })

function touches(...points: Array<[number, number]>) {
    return points.map(([clientX, clientY]) => ({ clientX, clientY })) as unknown as TouchList
}

describe('classifySwipe', () => {
    it.each([
        ['leftward flick turns forward', at(300), at(200, 10, 200), 390, 'next'],
        ['rightward flick turns back', at(100), at(200, -10, 200), 390, 'prev'],
        ['under 40px', at(300), at(265, 0, 100), 390, null],
        ['under 6% of a wide screen', at(1000), at(930, 0, 100), 1920, null],
        ['over 6% of a wide screen', at(1000), at(880, 0, 100), 1920, 'next'],
        ['mostly vertical', at(300), at(200, 60, 200), 390, null],
        ['long press', at(300), at(200, 0, 900), 390, null],
    ])('%s', (_name, start, end, width, expected) => {
        expect(classifySwipe(start, end, width)).toBe(expected)
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

    it('turns on a one-finger flick, ignoring a second finger, a selection and zoom', () => {
        let selected = false
        let zoomed = false
        const swipe = createSwipe({ hasSelection: () => selected, isZoomed: () => zoomed })
        swipe.start(start(300))
        expect(swipe.end(end(100), 390)).toBe('next')

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
