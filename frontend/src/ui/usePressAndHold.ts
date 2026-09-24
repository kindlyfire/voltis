import { onScopeDispose } from 'vue'

/**
 * Handlers for a button that repeats `fn` while held: once on press, then every `interval` ms
 * after `initialDelay`. Works with pointer and keyboard (Enter/Space), and a short press
 * (or a click from assistive tech) runs `fn` exactly once. Spread onto a button with `v-bind`.
 */
export function usePressAndHold(fn: () => void, { initialDelay = 400, interval = 100 } = {}) {
    let timer: ReturnType<typeof setTimeout> | undefined
    let holding = false

    function stop() {
        clearTimeout(timer)
        timer = undefined
        holding = false
    }

    function start() {
        stop()
        holding = true
        fn()
        const schedule = (delay: number) => {
            timer = setTimeout(() => {
                fn()
                schedule(interval)
            }, delay)
        }
        schedule(initialDelay)
    }

    onScopeDispose(stop)

    const isActivationKey = (e: KeyboardEvent) => e.key === 'Enter' || e.key === ' '
    // Pair with `focusableWhenDisabled`, so the button keeps focus when it hits its limit.
    const isInert = (e: Event) =>
        (e.currentTarget as Element | null)?.getAttribute?.('aria-disabled') === 'true'

    return {
        onPointerdown(e: PointerEvent) {
            if (e.button !== 0 || isInert(e)) return
            start()
            // A natively disabled button gets no pointerup.
            window.addEventListener('pointerup', stop, { once: true })
        },
        onPointerup: stop,
        onPointerleave: stop,
        onPointercancel: stop,
        onKeydown(e: KeyboardEvent) {
            if (!isActivationKey(e)) return
            // Also cancels the click that Enter would fire.
            e.preventDefault()
            if (!e.repeat && !holding && !isInert(e)) start()
        },
        onKeyup(e: KeyboardEvent) {
            if (!isActivationKey(e)) return
            // Also cancels the click that Space would fire.
            e.preventDefault()
            stop()
        },
        onBlur: stop,
        // Pointer and keyboard presses are handled above. `detail` is 0 only for synthetic
        // clicks (screen readers, `element.click()`).
        onClick(e: MouseEvent) {
            if (e.detail === 0 && !holding && !isInert(e)) fn()
        },
    }
}
