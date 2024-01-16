<template>
	<Suspense>
		<NuxtLayout>
			<ClientOnly>
				<NuxtPage />
			</ClientOnly>
		</NuxtLayout>
	</Suspense>
	<UNotifications />
	<div
		class="fixed bottom-0 pointer-events-none"
		id="bottom-screen-reference"
	></div>
</template>

<script lang="ts" setup>
import { useMeta, useUser } from './state/composables/queries'

const route = useRoute()
const qMeta = useMeta()
const qUser = useUser()
await Promise.all([qMeta.suspense(), qUser.suspense()])

async function checkPathAccess() {
	const meta = qMeta.data.value!
	const user = qUser.data.value
	// Force user creation redirect
	if (meta.forceUserCreation && route.path !== '/auth/register') {
		await navigateTo('/auth/register')
	}
	// Guest access disabled + user not logged in redirect
	else if (
		!meta.guestAccess &&
		!user &&
		route.path !== '/auth/login' &&
		route.path !== '/auth/register'
	) {
		await navigateTo('/auth/login')
	}
	// Admin dashboard not admin redirect
	else if (!user?.roles?.includes('admin') && route.path.startsWith('/admin')) {
		await navigateTo('/')
	}
}
await checkPathAccess()

watch(
	() => [route.fullPath, qUser.data.value],
	() => checkPathAccess(),
	{ immediate: true, deep: true }
)

onErrorCaptured(e => {
	console.error(e)
	return false
})
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

.padding-modal {
	@apply p-2 sm:p-4;
}
</style>
