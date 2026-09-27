import { useElementSize, useScroll, useWindowSize } from '@vueuse/core'
import { computed, type Ref } from 'vue'

/** How far the window has scrolled through the chapter's rendered height: 0
 * with its top at the top of the window, 1 with its bottom at the bottom. */
export function useChapterScrollProgress(host: Ref<HTMLElement | undefined>) {
    const { y } = useScroll(window)
    const { height: windowHeight } = useWindowSize()
    const { height } = useElementSize(host)

    return computed(() => {
        const el = host.value
        if (!el) return 0
        const top = el.getBoundingClientRect().top + window.scrollY
        const range = height.value - windowHeight.value
        if (range <= 0) return 1
        return Math.min(1, Math.max(0, (y.value - top) / range))
    })
}
