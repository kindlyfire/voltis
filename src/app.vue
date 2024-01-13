<template>
	<NuxtLayout>
		<NuxtPage />
	</NuxtLayout>
</template>

<script lang="ts" setup>
import { trpc } from './plugins/trpc'
import { useUser } from './state/composables/use-user'

const route = useRoute()
const qMeta = trpc.meta.useQuery()
const qUser = useUser()
await Promise.all([qMeta, qUser.suspense()])

watch(
	() => route.fullPath,
	() => {
		const meta = qMeta.data.value!
		if (meta.forceUserCreation && route.path !== '/auth/register') {
			navigateTo('/auth/register')
		} else if (
			!meta.guestAccess &&
			!qUser.data.value &&
			route.path !== '/auth/login' &&
			route.path !== '/auth/register'
		) {
			navigateTo('/auth/login')
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
