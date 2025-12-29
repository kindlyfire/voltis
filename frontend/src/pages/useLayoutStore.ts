import { useDebounceFn, useScroll } from '@vueuse/core'
import { acceptHMRUpdate, defineStore } from 'pinia'
import { computed, onBeforeMount, onUnmounted, ref, watch } from 'vue'

// Amount of scroll (in pixels) before hiding/showing the navbar
const SCROLL_HIDE_THRESHOLD = 200

// Navbar will always be shown when within this offset from the top
const SCROLL_MIN_OFFSET = 100

export const useLayoutStore = defineStore('layout', () => {
    const navbarScrollHideEnabled = ref(false)
    const scroll = useScroll(window)

    const anchorY = ref(0)
    const lastDirection = ref<'up' | 'down' | null>(null)
    const hidden = ref(false)

    const resetAnchor = useDebounceFn(() => {
        anchorY.value = scroll.y.value
        lastDirection.value = null
    }, 300)

    watch(
        () => scroll.y.value,
        (currentY, previousY) => {
            if (previousY === undefined) return

            if (currentY < SCROLL_MIN_OFFSET) {
                hidden.value = false
                anchorY.value = currentY
                lastDirection.value = null
                return
            }

            const direction = currentY > previousY ? 'down' : currentY < previousY ? 'up' : null
            if (direction && direction !== lastDirection.value) {
                anchorY.value = previousY
                lastDirection.value = direction
            }

            const delta = currentY - anchorY.value
            if (delta > SCROLL_HIDE_THRESHOLD) {
                hidden.value = true
            } else if (delta < -SCROLL_HIDE_THRESHOLD) {
                hidden.value = false
            }

            resetAnchor()
        }
    )

    const navbarHidden = computed(() => {
        if (!navbarScrollHideEnabled.value) return false
        return hidden.value
    })

    return {
        navbarScrollHideEnabled,
        navbarHidden,
    }
})

if (import.meta.hot) {
    import.meta.hot.accept(acceptHMRUpdate(useLayoutStore, import.meta.hot))
}

export function useNavbarScrollHide() {
    const store = useLayoutStore()

    onBeforeMount(() => {
        store.navbarScrollHideEnabled = true
    })

    onUnmounted(() => {
        store.navbarScrollHideEnabled = false
    })
}
