interface TouchPoint {
    x: number
    y: number
    time: number
}

/** Pinch-zoomed: a drag pans the zoomed view. */
export function isZoomed() {
    return (window.visualViewport?.scale ?? 1) > 1
}

const MIN_DISTANCE = 40
const MIN_FRACTION = 0.06
const MAX_DURATION = 800

/** A horizontal flick: far enough, mostly sideways and quick, so a long press
 * or a vertical pan never turns. */
export function classifySwipe(
    start: TouchPoint,
    end: TouchPoint,
    width: number
): 'next' | 'prev' | null {
    const dx = end.x - start.x
    const dy = end.y - start.y
    if (end.time - start.time > MAX_DURATION) return null
    if (Math.abs(dx) < Math.max(MIN_DISTANCE, MIN_FRACTION * width)) return null
    if (Math.abs(dx) <= 2 * Math.abs(dy)) return null
    return dx < 0 ? 'next' : 'prev'
}

type TouchLike = Pick<TouchEvent, 'touches' | 'changedTouches' | 'timeStamp'>

/** One finger only, and nothing selected or zoomed when it lifts: those
 * gestures mean something else. */
export function createSwipe<E extends TouchLike>(options: {
    hasSelection(e: E): boolean
    isZoomed(): boolean
}) {
    let start: TouchPoint | null = null
    return {
        start(e: E) {
            const touch = e.touches[0]
            start =
                e.touches.length === 1 && touch
                    ? { x: touch.clientX, y: touch.clientY, time: e.timeStamp }
                    : null
        },
        end(e: E, width: number): 'next' | 'prev' | null {
            const from = start
            const touch = e.changedTouches[0]
            if (e.touches.length === 0) start = null
            if (!from || !touch || e.touches.length > 0) return null
            if (options.hasSelection(e) || options.isZoomed()) return null
            return classifySwipe(
                from,
                { x: touch.clientX, y: touch.clientY, time: e.timeStamp },
                width
            )
        },
    }
}
