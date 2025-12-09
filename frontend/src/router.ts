import { createRouter, createWebHistory } from 'vue-router'
import AppLayout from './pages/_layout.vue'
import HomePage from './pages/HomePage.vue'
import LibraryPage from './pages/LibraryPage.vue'
import PageLogin from './pages/auth/PageLogin.vue'
import PageRegister from './pages/auth/PageRegister.vue'
import ContentPage from './pages/content/ContentPage.vue'

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
					name: 'content',
					component: ContentPage,
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
	],
})

export default router
