import { useDebounceFn } from '@vueuse/core'
import { createRouter, createWebHistory } from 'vue-router'
import AppLayout from './pages/_layout/Layout.vue'
import PageLogin from './pages/auth/PageLogin.vue'
import PageOidcComplete from './pages/auth/PageOidcComplete.vue'
import PageRegister from './pages/auth/PageRegister.vue'
import ContentPage from './pages/content/ContentPage.vue'
import HomePage from './pages/HomePage.vue'
import LibraryPage from './pages/LibraryPage.vue'
import ListPage from './pages/lists/ListPage.vue'
import ListsPage from './pages/lists/ListsPage.vue'
import NotFoundPage from './pages/NotFoundPage.vue'
import ReadPage from './pages/read/ReadPage.vue'
import SettingsAccountPage from './pages/settings/AccountPage.vue'
import SettingsBrokenRefsPage from './pages/settings/BrokenRefsPage.vue'
import SettingsGeneralPage from './pages/settings/GeneralPage.vue'
import SettingsInterfacePage from './pages/settings/InterfacePage.vue'
import SettingsLibrariesPage from './pages/settings/LibrariesPage.vue'
import SettingsMetadataPage from './pages/settings/MetadataPage.vue'
import SettingsOpdsPage from './pages/settings/OpdsPage.vue'
import SettingsTasksPage from './pages/settings/TasksPage.vue'
import SettingsUsersPage from './pages/settings/UsersPage.vue'
import { savedTop, trackSavedScroll } from './utils/savedScroll'
import { pendingRestoreTop, whenReachable } from './utils/whenReachable'

declare module 'vue-router' {
    interface RouteMeta {
        admin?: boolean
    }
}

const router = createRouter({
    history: createWebHistory(import.meta.env.BASE_URL),
    scrollBehavior(to, from, savedPosition) {
        // Query/hash-only changes (grid filters, MetadataPage tab/page/q, reader ?page=/?ch=) keep
        // the position. This relies on `useRouteQueryParams` using `router.replace`; with `push`,
        // Back through filter changes would stop restoring. `from.matched.length`: the first
        // navigation comes from START_LOCATION (`/`), and a reload of `/` must reach `savedPosition`.
        if (from.matched.length && to.path === from.path) return false
        if (savedPosition) {
            // The readers restore their own position from ?page= or the history locator.
            if (to.name === 'read-content') return false
            // The mirror wins: the router's copies can hold the position from before a restore.
            return whenReachable(
                { left: savedPosition.left, top: savedTop() ?? savedPosition.top },
                () => router.currentRoute.value.fullPath === to.fullPath
            )
        }
        return { top: 0 }
    },
    routes: [
        {
            path: '/',
            component: AppLayout,
            children: [
                {
                    path: '',
                    name: 'home',
                    component: HomePage,
                },
                {
                    path: '/:id(l_[^/]+)',
                    name: 'library',
                    component: LibraryPage,
                },
                {
                    path: '/:id(c_[^/]+)',
                    name: 'info-page',
                    component: ContentPage,
                },
                {
                    path: '/r/:id(c_[^/]+)',
                    name: 'read-content',
                    component: ReadPage,
                },
                {
                    path: '/lists',
                    name: 'lists',
                    component: ListsPage,
                },
                {
                    path: '/:id(cl_[^/]+)',
                    name: 'list',
                    component: ListPage,
                },
                {
                    path: '/settings/interface',
                    name: 'settings-interface',
                    component: SettingsInterfacePage,
                },
                {
                    path: '/settings/account',
                    name: 'settings-account',
                    component: SettingsAccountPage,
                },
                {
                    path: '/settings/opds',
                    name: 'settings-opds',
                    component: SettingsOpdsPage,
                },
                {
                    path: '/settings/general',
                    name: 'settings-general',
                    meta: { admin: true },
                    component: SettingsGeneralPage,
                },
                {
                    path: '/settings/users',
                    name: 'settings-users',
                    meta: { admin: true },
                    component: SettingsUsersPage,
                },
                {
                    path: '/settings/libraries',
                    name: 'settings-libraries',
                    meta: { admin: true },
                    component: SettingsLibrariesPage,
                },
                {
                    path: '/settings/metadata',
                    name: 'settings-metadata',
                    meta: { admin: true },
                    component: SettingsMetadataPage,
                },
                {
                    path: '/settings/broken-refs',
                    name: 'settings-broken-refs',
                    component: SettingsBrokenRefsPage,
                },
                {
                    path: '/settings/tasks',
                    name: 'settings-tasks',
                    meta: { admin: true },
                    component: SettingsTasksPage,
                },
                { path: '/settings', redirect: '/settings/interface' },
                { path: '/:pathMatch(.*)*', name: 'not-found', component: NotFoundPage },
            ],
        },
        {
            path: '/auth/login',
            name: 'login',
            component: PageLogin,
        },
        {
            path: '/auth/register',
            name: 'register',
            component: PageRegister,
        },
        {
            path: '/auth/oidc/complete',
            name: 'oidc-complete',
            component: PageOidcComplete,
        },
        // The kit gallery, the living spec of `src/ui` (dev only, no login needed).
        ...(import.meta.env.DEV
            ? [{ path: '/_kit', name: 'kit', component: () => import('./pages/_kit/KitPage.vue') }]
            : []),
    ],
})

// `scrollBehavior` turns off native restoration, and vue-router's save on page hide doesn't
// survive a reload in Chrome, so keep the entry's `scroll` (read back on reload) current.
const saveScroll = useDebounceFn(() => {
    // Until the restore lands, `scrollY` is the position from before it.
    if (pendingRestoreTop() !== null) return
    history.replaceState({ ...history.state, scroll: { left: scrollX, top: scrollY } }, '')
}, 200)
window.addEventListener('scroll', saveScroll, { passive: true })
// A pending save would land on the next page's history entry.
router.beforeEach(() => saveScroll.cancel())
// Navigations (including query-only `replace`s) reset the entry's `scroll`.
router.afterEach(() => saveScroll())

// After vue-router's own `pagehide` save, which records the position from before a pending
// restore. Best-effort: Chrome may drop it, as noted above.
window.addEventListener('pagehide', () => {
    const top = pendingRestoreTop()
    if (top !== null) history.replaceState({ ...history.state, scroll: { left: 0, top } }, '')
})

trackSavedScroll(router, pendingRestoreTop)

export default router
