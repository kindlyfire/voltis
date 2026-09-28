import { useEventListener } from '@vueuse/core'
import type { Ref } from 'vue'

/** Past this, the direction is judged. */
const JUDGE_PX = 4
/** Past this, it's a drag rather than a tap. */
const TAP_SLOP_PX = 10
/** px/ms toward the edge. */
const FLING_VELOCITY = 0.4
const CLOSE_FRACTION = 0.35
/** Older than this, the last move's velocity is a paused finger's: none. */
const STALE_MS = 100

/** Content that needs horizontal drags (like `ASlider`) opts out with `data-no-drawer-swipe`. */
const EXCLUDED = 'input, textarea, select, [contenteditable], [data-no-drawer-swipe]'

type Gesture = {
    startX: number
    startY: number
    /** Largest |dx| since `touchstart`. */
    maxDx: number
    moves: number
    candidate: boolean
    locked: boolean
    lockX: number
    width: number
    offset: number
    lastX: number
    lastTime: number
    /** Of the last move, toward the edge. */
    velocity: number
}

/**
 * Touch drag on the panel toward the drawer's own edge: the panel follows the finger, and release
 * closes it or snaps back. Touch events rather than pointer events, so vertical scrolling in the
 * nested scrollers stays native without `touch-action` on each. Renders through `--drawer-drag`
 * and `--drawer-progress` rather than an inline transform, which would override the leave
 * transition.
 */
export function useDrawerSwipe({
    panel,
    scrim,
    side,
    onClose,
}: {
    panel: Readonly<Ref<HTMLElement | null>>
    scrim: Readonly<Ref<HTMLElement | null>>
    side: () => 'left' | 'right'
    onClose: () => void
}) {
    let g: Gesture | null = null
    const toward = () => (side() === 'right' ? 1 : -1)

    function elements() {
        return [panel.value, scrim.value].filter(el => !!el)
    }

    function render(offset: number, width: number) {
        for (const el of elements()) {
            el.style.setProperty('--drawer-drag', `${offset * toward()}px`)
            el.style.setProperty('--drawer-progress', String(width ? offset / width : 0))
        }
    }

    function setDragging(on: boolean) {
        for (const el of elements()) el.classList.toggle('dragging', on)
    }

    function snapBack() {
        setDragging(false)
        for (const el of elements()) {
            el.style.removeProperty('--drawer-drag')
            el.style.removeProperty('--drawer-progress')
        }
    }

    function abort() {
        if (g?.locked) snapBack()
        g = null
    }

    useEventListener(panel, 'touchstart', (e: TouchEvent) => {
        const t = e.touches[0]
        if (e.touches.length !== 1 || !t || (e.target as Element).closest(EXCLUDED)) return abort()
        g = {
            startX: t.clientX,
            startY: t.clientY,
            maxDx: 0,
            moves: 0,
            candidate: false,
            locked: false,
            lockX: 0,
            width: 0,
            offset: 0,
            lastX: t.clientX,
            lastTime: e.timeStamp,
            velocity: 0,
        }
    })

    useEventListener(
        panel,
        'touchmove',
        (e: TouchEvent) => {
            const t = e.touches[0]
            if (!g || !t) return
            if (e.touches.length !== 1) return abort()
            g.moves++
            const dx = (t.clientX - g.startX) * toward()
            const dy = t.clientY - g.startY
            g.maxDx = Math.max(g.maxDx, Math.abs(dx))
            const dt = e.timeStamp - g.lastTime
            if (dt > 0) {
                g.velocity = ((t.clientX - g.lastX) * toward()) / dt
                g.lastX = t.clientX
                g.lastTime = e.timeStamp
            }

            if (!g.candidate) {
                if (Math.max(Math.abs(dx), Math.abs(dy)) <= JUDGE_PX) return
                // Anything else stays native for the rest of the gesture.
                if (Math.abs(dx) <= Math.abs(dy) || dx <= 0) {
                    g = null
                    return
                }
                g.candidate = true
            }
            if (!g.locked) {
                // Preventing the first move can make Gecko drop the click of a wobbly tap.
                if (g.moves === 1 && g.maxDx <= TAP_SLOP_PX) return
                g.locked = true
                g.lockX = t.clientX
                g.width = panel.value?.getBoundingClientRect().width ?? 0
                setDragging(true)
            }
            // The browser is already scrolling.
            if (!e.cancelable) return abort()
            e.preventDefault()
            g.offset = Math.min(Math.max((t.clientX - g.lockX) * toward(), 0), g.width)
            render(g.offset, g.width)
        },
        { passive: false }
    )

    useEventListener(panel, 'touchend', (e: TouchEvent) => {
        const gesture = g
        g = null
        if (!gesture?.locked) return
        if (gesture.maxDx <= TAP_SLOP_PX) return snapBack()
        // No click or mouse events: nothing under the finger activates, before or after closing.
        e.preventDefault()
        const v = e.timeStamp - gesture.lastTime > STALE_MS ? 0 : gesture.velocity
        const far = gesture.offset > CLOSE_FRACTION * gesture.width
        const close = v > FLING_VELOCITY || (far && v > -FLING_VELOCITY)
        if (!close) return snapBack()
        // Keeps the variables: the leave transition starts from the dragged offset.
        setDragging(false)
        onClose()
    })

    useEventListener(panel, 'touchcancel', abort)
}
