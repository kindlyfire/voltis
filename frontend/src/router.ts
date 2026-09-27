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
import ReadPage from './pages/read/ReadPage.vue'
import SettingsAccountPage from './pages/settings/AccountPage.vue'
import SettingsBrokenRefsPage from './pages/settings/BrokenRefsPage.vue'
import SettingsGeneralPage from './pages/settings/GeneralPage.vue'
import SettingsInterfacePage from './pages/settings/InterfacePage.vue'
import SettingsLibrariesPage from './pages/settings/LibrariesPage.vue'
import SettingsMetadataPage from './pages/settings/MetadataPage.vue'
import SettingsTasksPage from './pages/settings/TasksPage.vue'
import SettingsUsersPage from './pages/settings/UsersPage.vue'

const router = createRouter({
    history: createWebHistory(import.meta.env.BASE_URL),
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
                    path: '/settings/general',
                    name: 'settings-general',
                    component: SettingsGeneralPage,
                },
                {
                    path: '/settings/users',
                    name: 'settings-users',
                    component: SettingsUsersPage,
                },
                {
                    path: '/settings/libraries',
                    name: 'settings-libraries',
                    component: SettingsLibrariesPage,
                },
                {
                    path: '/settings/metadata',
                    name: 'settings-metadata',
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
                    component: SettingsTasksPage,
                },
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

export default router
