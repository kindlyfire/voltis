<template>
	<VApp>
		<VAppBar>
			<VAppBarNavIcon class="d-md-none" @click="drawer = !drawer" />
			<VAppBarTitle>Voltis</VAppBarTitle>
			<VSpacer />
			<VMenu v-if="me.data?.value">
				<template #activator="{ props }">
					<VBtn v-bind="props" variant="text" class="me-5">
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
					<VBtn variant="text" class="me-5">Login</VBtn>
				</RouterLink>
			</template>
		</VAppBar>
		<VNavigationDrawer v-model="drawer" :permanent="mdAndUp" :temporary="!mdAndUp">
			<VList nav>
				<VListItem to="/" exact prepend-icon="mdi-home">
					<VListItemTitle>Home</VListItemTitle>
				</VListItem>
				<VDivider class="my-2" />
				<VListSubheader>Libraries</VListSubheader>
				<VListItem
					v-for="library in libraries.data?.value"
					:key="library.id"
					:to="`/${library.id}`"
					prepend-icon="mdi-bookshelf"
				>
					<VListItemTitle>{{ library.name }}</VListItemTitle>
				</VListItem>
				<VDivider class="my-2" />
				<VListItem to="/settings" prepend-icon="mdi-cog">
					<VListItemTitle>Settings</VListItemTitle>
				</VListItem>
			</VList>
		</VNavigationDrawer>
		<VMain>
			<RouterView />
		</VMain>
	</VApp>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useDisplay } from 'vuetify'
import { usersApi } from '@/utils/api/users'
import { authApi } from '@/utils/api/auth'
import { librariesApi } from '@/utils/api/libraries'
import { useRouter } from 'vue-router'

const router = useRouter()
const { mdAndUp } = useDisplay()
const drawer = ref(true)
const me = usersApi.useMe()
const logout = authApi.useLogout()
const libraries = librariesApi.useList()

async function handleLogout() {
	await logout.mutateAsync()
	router.push('/auth/login')
}
</script>
