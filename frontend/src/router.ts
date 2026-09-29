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
            return whenReachable(to.fullPath, savedPosition)
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

/** Resolves once the page is tall enough to reach `pos` and its height held for a frame (grids
 * render a single column for their first frame), after ~1s at most, or `false` if the user
 * navigated meanwhile. */
function whenReachable(fullPath: string, pos: { left: number; top: number }) {
    return new Promise<typeof pos | false>(resolve => {
        const deadline = performance.now() + 1000
        let lastHeight = -1
        const tick = () => {
            if (router.currentRoute.value.fullPath !== fullPath) return resolve(false)
            const height = document.documentElement.scrollHeight
            const settled = height === lastHeight && height - window.innerHeight >= pos.top
            if (settled || performance.now() >= deadline) return resolve(pos)
            lastHeight = height
            requestAnimationFrame(tick)
        }
        tick()
    })
}

// `scrollBehavior` turns off native restoration, and vue-router's save on page hide doesn't
// survive a reload in Chrome, so keep the entry's `scroll` (read back on reload) current.
const saveScroll = useDebounceFn(() => {
    history.replaceState({ ...history.state, scroll: { left: scrollX, top: scrollY } }, '')
}, 200)
window.addEventListener('scroll', saveScroll, { passive: true })
// A pending save would land on the next page's history entry.
router.beforeEach(() => saveScroll.cancel())
// Navigations (including query-only `replace`s) reset the entry's `scroll`.
router.afterEach(() => saveScroll())

export default router
