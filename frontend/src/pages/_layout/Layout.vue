<template>
    <a href="#main" class="skip-link" @click.prevent="skipToMain">Skip to content</a>

    <aside v-if="store.sidebarPersistent" id="sidebar" class="sidebar" aria-label="Sidebar">
        <!-- Header height, so the toggle doesn't move when it swaps into the header. -->
        <div class="brand mb-3.5 h-(--header-height) px-0.5">
            <AIconButton
                id="sidebar-toggle"
                :icon="IconMenu"
                label="Hide sidebar"
                aria-expanded="true"
                aria-controls="sidebar"
                @click="toggleSidebar"
            />
            <RouterLink to="/" class="wordmark a-focus rounded-md">Voltis</RouterLink>
        </div>
        <SidebarNav />
    </aside>

    <header
        class="header"
        :class="{
            'is-hidden': store.navbarHidden.value,
            'has-menu': !store.sidebarPersistent,
            'is-compact': compactSearch && !searchOpen,
        }"
    >
        <!-- On narrow screens the search takes over the whole header. -->
        <template v-if="compactSearch && searchOpen">
            <AIconButton :icon="IconArrowLeft" label="Close search" @click="closeSearch" />
            <SearchBox autofocus class="flex-1" @close="closeSearch" />
        </template>
        <template v-else>
            <div v-if="!store.sidebarPersistent" class="brand">
                <AIconButton
                    id="sidebar-toggle"
                    :icon="IconMenu"
                    :label="store.sidebarTemporary.value ? 'Menu' : 'Show sidebar'"
                    :aria-expanded="store.sidebarOpen"
                    @click="toggleSidebar"
                />
                <RouterLink to="/" class="wordmark a-focus rounded-md">Voltis</RouterLink>
            </div>
            <AIconButton
                v-if="compactSearch"
                id="search-open"
                :icon="IconMagnify"
                label="Search"
                aria-keyshortcuts="Control+K"
                class="ml-auto"
                @click="searchOpen = true"
            />
            <SearchBox v-else class="nav:max-w-[560px] flex-1" />
            <ScanIndicator />
        </template>
    </header>

    <main id="main" tabindex="-1" class="main outline-none">
        <RouterView v-slot="{ Component, route: r }">
            <NotFoundPage v-if="r.meta.admin && !isAdmin" forbidden />
            <component :is="Component" v-else />
        </RouterView>
    </main>

    <ADrawer
        v-if="store.sidebarTemporary.value"
        :open="store.sidebarOpen"
        nav
        title="Navigation"
        width="var(--sidebar-width)"
        @update:open="store.setSidebarOpen"
    >
        <div class="flex flex-1 flex-col px-3 pt-2 pb-3.5">
            <SidebarNav />
        </div>
    </ADrawer>

    <Teleport to="#overlays">
        <ModalContainer />
    </Teleport>
    <AToastRegion />
</template>

<script setup lang="ts">
import { useEventListener, useMediaQuery } from '@vueuse/core'
import { computed, nextTick, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import ADrawer from '@/ui/ADrawer.vue'
import AIconButton from '@/ui/AIconButton.vue'
import AToastRegion from '@/ui/AToastRegion.vue'
import { IconArrowLeft, IconMagnify, IconMenu } from '@/ui/icons'
import { usersApi } from '@/utils/api/users'
import { ModalContainer } from '@/utils/modals'
import NotFoundPage from '../NotFoundPage.vue'
import ScanIndicator from './ScanIndicator.vue'
import SearchBox from './SearchBox.vue'
import SidebarNav from './SidebarNav.vue'
import { useLayoutStore } from './useLayoutStore'

const store = useLayoutStore()
const route = useRoute()
// App mounts the layout only once `me` has loaded, so this never races the query.
const qMe = usersApi.useMe()
const isAdmin = computed(() => !!qMe.data.value?.permissions.includes('ADMIN'))

const compactSearch = useMediaQuery('(width < 40rem)')
const searchOpen = ref(false)
watch(compactSearch, () => (searchOpen.value = false))

// The temporary drawer and the compact search close on navigation (the reader drawers don't).
watch(
    () => route.fullPath,
    () => {
        store.closeDrawer()
        searchOpen.value = false
    }
)

async function closeSearch() {
    searchOpen.value = false
    await nextTick()
    document.getElementById('search-open')?.focus()
}

// SearchBox handles Ctrl K itself while it's mounted.
useEventListener(window, 'keydown', (e: KeyboardEvent) => {
    if (!compactSearch.value || searchOpen.value) return
    if (!e.ctrlKey || e.altKey || e.metaKey || e.key.toLowerCase() !== 'k') return
    e.preventDefault()
    searchOpen.value = true
})

function skipToMain() {
    document.getElementById('main')?.focus()
}

// Hiding or showing the persistent sidebar swaps its toggle between the sidebar and the header
// (only one exists at a time): keep focus on it.
async function toggleSidebar() {
    const persistent = !store.sidebarTemporary.value
    store.setSidebarOpen(!store.sidebarOpen)
    if (!persistent) return
    await nextTick()
    document.getElementById('sidebar-toggle')?.focus()
}
</script>

<style scoped>
@layer ui {
    .skip-link {
        position: fixed;
        top: 8px;
        left: 8px;
        z-index: calc(var(--z-header) + 1);
        padding: 10px 16px;
        border-radius: var(--radius-field);
        background: var(--color-inverse);
        color: var(--color-on-inverse);
        font-size: 14px;
        font-weight: 600;
        outline: 2px solid var(--color-inverse-primary);
        outline-offset: 2px;

        &:not(:focus) {
            clip-path: inset(50%);
            width: 1px;
            height: 1px;
            padding: 0;
            overflow: hidden;
            white-space: nowrap;
        }
    }

    .sidebar {
        position: fixed;
        inset: 0 auto 0 0;
        z-index: var(--z-sidebar);
        display: flex;
        flex-direction: column;
        width: var(--sidebar-width);
        height: 100dvh;
        overflow-y: auto;
        padding: 0 12px 14px;
        padding-left: calc(12px + env(safe-area-inset-left));
        background: var(--color-sidebar);
        box-shadow: inset -1px 0 0 var(--color-outline-variant);
    }

    /* The menu toggle and the wordmark, in the sidebar or the header: the same spacing, so the
     * wordmark doesn't move when the toggle swaps between them. */
    .brand {
        display: flex;
        flex: none;
        align-items: center;
        gap: 10px;
    }

    .wordmark {
        flex: none;
        font-family: var(--font-display);
        font-size: 23px;
        font-weight: 600;
        line-height: 1.2;
    }

    /* Scroll-hide slides it away but keeps `--layout-top`; the reader's temporary chrome sets
     * that to 0. */
    .header {
        position: fixed;
        top: 0;
        right: 0;
        left: var(--layout-left);
        z-index: var(--z-header);
        display: flex;
        align-items: center;
        gap: 16px;
        height: var(--header-height);
        padding: 0 40px;
        background: var(--color-bg);
        transition: transform var(--duration-medium) var(--ease-standard);

        /* Keyboard focus brings it back. */
        &.is-hidden:not(:has(:focus-visible)) {
            transform: translateY(-100%);
        }

        /* The menu button lines up with the sidebar's. */
        &.has-menu {
            padding-left: 14px;
        }
    }

    .main {
        display: block;
        min-height: 100dvh;
        padding-top: var(--layout-top);
        padding-left: var(--layout-left);
    }

    /* Icon buttons carry their own inset, so an edge with one gets less padding than one with the
     * search pill. */
    @media (width < 60rem) {
        .header {
            gap: 8px;

            &,
            &.has-menu {
                padding: 0 12px 0 4px;
            }

            &:not(.is-compact) > :deep([data-scan-trigger]) {
                margin-right: -8px;
            }

            &.is-compact {
                gap: 4px;
                padding: 0 4px;
            }
        }
    }
}
</style>
