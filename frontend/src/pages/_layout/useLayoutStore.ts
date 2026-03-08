import { useDebounceFn, useScroll } from '@vueuse/core'
import { acceptHMRUpdate, defineStore } from 'pinia'
import { computed, onBeforeMount, onUnmounted, ref, watch } from 'vue'
import { useDisplay, useTheme } from 'vuetify'
import { useLocalStorage } from '@/utils/localStorage'
import { createOverridableValue, useSystemTheme } from '@/utils/misc'

export const useLayoutStore = defineStore('layout', () => {
    const { mdAndUp } = useDisplay(undefined, 'composables')

    // Navbar stuff
    const navbarScrollHide = {
        enabled: ref(false),
        hidden: useNavbarScrollHideState(),
    }
    const navbarHidden = createOverridableValue(false, [
        // Hide when scrolling down a while
        'scrollHide',
        // Paged mode always hides sidebar
        'comicReaderPaged',
        // Opening the reader sidebar always shows the navbar
        'comicReaderSidebar',
    ])
    watch(
        () => [navbarScrollHide.enabled.value, navbarScrollHide.hidden.value],
        ([enabled, hidden]) => {
            navbarHidden.setLayer('scrollHide', enabled ? hidden : undefined)
        }
    )
    const navbarTemporary = computed(() => {
        // Navbar scroll hide doesn't count, since it will always show when the
        // viewport is near the top of the scroll area
        return navbarHidden.getLayer('comicReaderPaged') || false
    })

    /** sidebarTemporary has the default state (true on mobile), and an override
     * (reader pages make the sidebar temporary). temporary = uses an overlay
     * instead of taking up space in the layout */
    const sidebarTemporary = createOverridableValue(() => !mdAndUp.value, ['comicReader'])

    /** sidebarOpen has the default state (hidden on mobile), and an override
     * (clicking the sidebar icon should show it) */
    const sidebarOpen = createOverridableValue(() => mdAndUp.value, ['manual'])
    function setSidebarOpen(state: boolean) {
        console.log('setSidebarOpen', state)
        sidebarOpen.setLayer(
            'manual',
            state === sidebarOpen.initialValue.value ? undefined : state!
        )
    }

    // Theme
    const vuetifyTheme = useTheme()
    const systemTheme = useSystemTheme()
    const { value: themePreference } = useLocalStorage<'light' | 'dark' | null>(
        'theme-preference',
        found => found ?? null
    )
    const effectiveTheme = computed(() => {
        if (themePreference.value) {
            return themePreference.value
        } else {
            return systemTheme.value ? 'dark' : 'light'
        }
    })
    watch(
        () => effectiveTheme.value,
        theme => {
            vuetifyTheme.change(theme)
        },
        { immediate: true }
    )
    function toggleTheme() {
        themePreference.value = effectiveTheme.value === 'dark' ? 'light' : 'dark'
    }
    function resetTheme() {
        themePreference.value = null
    }

    return {
        navbarScrollHide,
        navbarHidden,
        navbarTemporary,

        // Sidebar
        sidebarOpen,
        setSidebarOpen,
        sidebarTemporary,

        // Theme
        theme: effectiveTheme,
        toggleTheme,
        resetTheme,
    }
})

if (import.meta.hot) {
    import.meta.hot.accept(acceptHMRUpdate(useLayoutStore, import.meta.hot))
}

export function useNavbarScrollHide() {
    const layout = useLayoutStore()
    onBeforeMount(() => {
        layout.navbarScrollHide.enabled = true
    })
    onUnmounted(() => {
        layout.navbarScrollHide.enabled = false
    })
}

// Amount of scroll (in pixels) before hiding/showing the navbar
const SCROLL_HIDE_THRESHOLD = 200

// Navbar will always be shown when within this offset from the top
const SCROLL_MIN_OFFSET = 100

/** Computes based on scroll position changes whether the navbar should be
 * hidden or not. Result used by the store only when enabled. */
function useNavbarScrollHideState() {
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

    return hidden
}
