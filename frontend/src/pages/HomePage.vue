<template>
	<VApp>
		<VAppBar>
			<VAppBarTitle>Voltis</VAppBarTitle>
			<VSpacer />
			<VMenu v-if="me.data?.value">
				<template #activator="{ props }">
					<VBtn v-bind="props" variant="text">
						{{ me.data.value.username }}
						<VIcon end>mdi-chevron-down</VIcon>
					</VBtn>
				</template>
				<VList>
					<VListItem @click="handleLogout" :disabled="logout.isPending.value">
						<VListItemTitle>Logout</VListItemTitle>
					</VListItem>
				</VList>
			</VMenu>
			<template v-else>
				<RouterLink to="/auth/login">
					<VBtn variant="text">Login</VBtn>
				</RouterLink>
			</template>
		</VAppBar>
		<VMain>
			<VContainer>
				<h1 class="text-xl">Hello, World!</h1>
			</VContainer>
		</VMain>
	</VApp>
</template>

<script setup lang="ts">
import { usersApi } from '@/utils/api/users'
import { authApi } from '@/utils/api/auth'
import { useRouter } from 'vue-router'

const router = useRouter()
const me = usersApi.useMe()
const logout = authApi.useLogout()

async function handleLogout() {
	await logout.mutateAsync()
	router.push('/auth/login')
}
</script>
