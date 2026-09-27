export interface WheelSample {
    deltaX: number
    deltaY: number
    deltaMode: number
    timeStamp: number
    ctrlKey: boolean
    /** Legacy, but the one field that tells a mouse notch (a multiple of 120)
     * apart from a touchpad or high-resolution wheel step. */
    wheelDeltaX?: number
    wheelDeltaY?: number
}

const QUIET_MS = 180
const REACCEL_FACTOR = 1.5
const REACCEL_MIN_MS = 80
const TURN_DISTANCE = 30
const RECENT = 4

function isNotch(e: WheelSample, lines: boolean, vertical: boolean): boolean {
    if (lines) return true
    const legacy = Math.abs((vertical ? e.wheelDeltaY : e.wheelDeltaX) ?? 0)
    return legacy > 0 && legacy % 120 === 0
}

/** Every mouse notch turns a page. Smooth scrolling turns once per gesture: a
 * trackpad flick and its inertia arrive as dozens of events, so a gesture ends
 * only on quiet, a change of direction, or a new flick rising out of the
 * previous one's inertia. A notch counts as its gesture's turn, and a gesture
 * that has had one smooth event stays smooth until quiet, so a smooth step that
 * happens to be a multiple of 120 can't turn an extra page. */
export function createWheelTurns() {
    let lastTime = -Infinity
    let lastTurn = -Infinity
    let direction = 0
    let distance = 0
    let turned = false
    let smooth = false
    const recent: number[] = []

    return (e: WheelSample): 'next' | 'prev' | null => {
        // Read before the deltas: Firefox reports line-mode wheels in pixels
        // to pages that don't check `deltaMode` first.
        const lines = e.deltaMode !== 0
        if (e.ctrlKey) return null
        const vertical = Math.abs(e.deltaY) >= Math.abs(e.deltaX)
        const delta = vertical ? e.deltaY : e.deltaX
        if (!delta) return null
        const quiet = e.timeStamp - lastTime > QUIET_MS
        if (quiet) smooth = false
        smooth ||= !isNotch(e, lines, vertical)
        const size = Math.abs(delta)
        const reaccelerated =
            turned &&
            size > REACCEL_FACTOR * Math.max(...recent) &&
            e.timeStamp - lastTurn >= REACCEL_MIN_MS
        if (!smooth || quiet || Math.sign(delta) !== direction || reaccelerated) {
            distance = 0
            turned = false
            recent.length = 0
        }
        lastTime = e.timeStamp
        direction = Math.sign(delta)
        recent.push(size)
        if (recent.length > RECENT) recent.shift()
        if (turned) return null
        distance += size
        if (smooth && distance < TURN_DISTANCE) return null
        turned = true
        lastTurn = e.timeStamp
        return delta > 0 ? 'next' : 'prev'
    }
}
