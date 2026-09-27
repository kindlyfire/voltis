import { describe, expect, it } from 'vitest'
import { createWheelTurns, type WheelSample } from './wheelTurns'

/** A trackpad flick as Chrome reports it at 60 Hz: a short rise, then inertia
 * decaying over about half a second. */
const FLICK = [
    3, 9, 22, 41, 48, 44, 38, 32, 27, 23, 19, 16, 13, 11, 9, 8, 6, 5, 4, 3, 3, 2, 2, 1, 1, 1, 1,
]

function trace(deltas: number[], from = 0, step = 16, extra: Partial<WheelSample> = {}) {
    return deltas.map((deltaY, i) => ({
        deltaX: 0,
        deltaY,
        deltaMode: 0,
        timeStamp: from + i * step,
        ctrlKey: false,
        ...extra,
    }))
}

/** Chrome reports a mouse notch as 100px, and the legacy field as 120. */
const notch = (t: number, deltaY = 100) =>
    trace([deltaY], t, 16, { wheelDeltaY: -1.2 * deltaY })[0]!

function turns(samples: WheelSample[]) {
    const classify = createWheelTurns()
    return samples.map(classify).filter(Boolean)
}

describe('wheel turns', () => {
    it('turns once for a trackpad flick and its inertia', () => {
        expect(turns(trace(FLICK))).toEqual(['next'])
    })

    it('turns twice for a second flick rising out of the first one inertia', () => {
        const first = trace(FLICK.slice(0, 16))
        const second = trace(FLICK, 16 * 16)
        expect(turns([...first, ...second])).toEqual(['next', 'next'])
    })

    it('turns once per mouse notch, however fast the spin', () => {
        expect(turns([0, 300, 600].map(t => notch(t)))).toEqual(['next', 'next', 'next'])
        expect(turns([0, 16, 32, 48].map(t => notch(t, -100)))).toEqual([
            'prev',
            'prev',
            'prev',
            'prev',
        ])
    })

    it('treats high-resolution wheel steps as smooth scrolling', () => {
        const steps = trace(Array(8).fill(12.5), 0, 16, { wheelDeltaY: -15 })
        expect(turns(steps)).toEqual(['next'])
    })

    it("doesn't take an inertia step for a notch", () => {
        // The 40px inertia step reports exactly 120.
        const deltas = FLICK.map((d, i) => (i === 6 ? 40 : d))
        const flick = trace(deltas).map(sample => ({ ...sample, wheelDeltaY: -3 * sample.deltaY }))
        expect(turns(flick)).toEqual(['next'])
    })

    it('counts a notch-looking first step as the smooth gesture turn', () => {
        // A touchpad flick whose 40px first event reports exactly 120.
        const flick = trace([40, ...FLICK.slice(4)]).map(sample => ({
            ...sample,
            wheelDeltaY: -3 * sample.deltaY,
        }))
        expect(turns(flick)).toEqual(['next'])
        expect(turns([notch(0), ...trace(FLICK, 16)])).toEqual(['next'])
    })

    it('reads deltaMode before the deltas', () => {
        const reads: string[] = []
        const sample = new Proxy(notch(0), {
            get(target, key: keyof WheelSample) {
                reads.push(key)
                return target[key]
            },
        })
        createWheelTurns()(sample)
        expect(reads.indexOf('deltaMode')).toBeLessThan(reads.indexOf('deltaY'))
    })

    it('starts a new gesture when the direction flips', () => {
        const back = trace(
            FLICK.map(d => -d),
            5 * 16
        )
        expect(turns([...trace(FLICK.slice(0, 5)), ...back])).toEqual(['next', 'prev'])
    })

    it('ignores pinch zoom', () => {
        expect(turns(trace(FLICK, 0, 16, { ctrlKey: true }))).toEqual([])
    })

    it('reads Firefox line and page deltas as notches', () => {
        expect(turns(trace([3, 3, -1], 0, 16, { deltaMode: 1 }))).toEqual(['next', 'next', 'prev'])
        expect(turns(trace([1], 0, 16, { deltaMode: 2 }))).toEqual(['next'])
    })

    it('follows the dominant axis', () => {
        const sideways = trace(FLICK).map(sample => ({
            ...sample,
            deltaX: -sample.deltaY,
            deltaY: 2,
        }))
        expect(turns(sideways)).toEqual(['prev'])
    })
})
