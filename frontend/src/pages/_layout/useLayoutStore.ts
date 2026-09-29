import { useDebounceFn, useMediaQuery, useScroll } from '@vueuse/core'
import { acceptHMRUpdate, defineStore } from 'pinia'
import { computed, onBeforeMount, onUnmounted, ref, watch } from 'vue'
import { useLocalStorage } from '@/utils/localStorage'
import { createOverridableValue, useSystemTheme } from '@/utils/misc'

export const useLayoutStore = defineStore('layout', () => {
    /** Matches `--breakpoint-nav` (60rem). */
    const mdAndUp = useMediaQuery('(min-width: 960px)')

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
        // The book reader hides all chrome
        'bookReader',
        // ...until its drawer is opened
        'bookReaderSidebar',
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
        return (
            navbarHidden.getLayer('comicReaderPaged') ||
            navbarHidden.getLayer('bookReader') ||
            false
        )
    })

    // The space the header reserves: scroll-hide keeps it, reader-temporary chrome gives it up.
    // Published on :root for CSS and `getLayoutTop()`.
    watch(
        navbarTemporary,
        temporary => {
            document.documentElement.style.setProperty('--layout-top', temporary ? '0px' : '')
        },
        // Sync (as is `--layout-left` below), so a page mounting in the same flush measures the
        // new geometry.
        { immediate: true, flush: 'sync' }
    )

    /** Temporary: the sidebar is a drawer over the page instead of a column beside it. True on
     * mobile, and forced by the reader pages. */
    const sidebarTemporary = createOverridableValue(
        () => !mdAndUp.value,
        ['comicReader', 'bookReader']
    )

    /** The persistent sidebar's collapse button hides it; the choice persists. */
    const { value: sidebarCollapsed } = useLocalStorage<boolean>(
        'sidebar-collapsed',
        found => found === true
    )
    /** The temporary drawer starts closed whenever the sidebar becomes temporary. */
    const drawerOpen = ref(false)
    watch(sidebarTemporary.value, () => (drawerOpen.value = false))

    const sidebarOpen = computed(() =>
        sidebarTemporary.value.value ? drawerOpen.value : !sidebarCollapsed.value
    )
    function setSidebarOpen(open: boolean) {
        if (sidebarTemporary.value.value) drawerOpen.value = open
        else sidebarCollapsed.value = !open
    }

    /** The sidebar is shown as a column beside the page. */
    const sidebarPersistent = computed(() => !sidebarTemporary.value.value && sidebarOpen.value)

    // The column the persistent sidebar takes, next to `--layout-top`.
    watch(
        sidebarPersistent,
        persistent => {
            document.documentElement.style.setProperty(
                '--layout-left',
                persistent ? 'var(--sidebar-width)' : ''
            )
        },
        { immediate: true, flush: 'sync' }
    )

    // Theme
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
            document.documentElement.classList.toggle('dark', theme === 'dark')
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
        sidebarPersistent,
        closeDrawer: () => (drawerOpen.value = false),

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
