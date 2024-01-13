<template>
	<NuxtLayout>
		<NuxtPage />
	</NuxtLayout>
</template>

<script lang="ts" setup>
import { trpc } from './plugins/trpc'
import { useUser } from './state/composables/queries'

const route = useRoute()
const qMeta = trpc.meta.useQuery()
const qUser = useUser()
await Promise.all([qMeta, qUser.suspense()])

watch(
	() => [route.fullPath, qUser.data.value],
	() => {
		const meta = qMeta.data.value!
		const user = qUser.data.value
		// Force user creation redirect
		if (meta.forceUserCreation && route.path !== '/auth/register') {
			navigateTo('/auth/register')
		}
		// Guest access disabled + user not logged in redirect
		else if (
			!meta.guestAccess &&
			!user &&
			route.path !== '/auth/login' &&
			route.path !== '/auth/register'
		) {
			navigateTo('/auth/login')
		}
		// Admin dashboard not admin redirect
		else if (
			!user?.roles?.includes('admin') &&
			route.path.startsWith('/admin')
		) {
			navigateTo('/')
		}
	},
	{ immediate: true, deep: true }
)
</script>

<style>
.acontainer {
	width: 100%;
	max-width: 1400px;
	margin: 0 auto;
	padding: 0 8px;
}

.acontainer-xs {
	width: 100%;
	max-width: 400px;
	margin: 0 auto;
	padding: 0 8px;
}
</style>
