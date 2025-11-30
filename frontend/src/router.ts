import { createRouter, createWebHistory } from 'vue-router'
import HomePage from './pages/HomePage.vue'
import PageLogin from './pages/auth/PageLogin.vue'
import PageRegister from './pages/auth/PageRegister.vue'

const router = createRouter({
	history: createWebHistory(import.meta.env.BASE_URL),
	routes: [
		{
			path: '/',
			name: 'home',
			component: HomePage,
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
