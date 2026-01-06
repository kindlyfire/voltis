<template>
    <VApp>
        <VAppBar
            :flat="store.navbarHidden"
            :style="store.navbarHidden && { transform: 'translateY(-64px)' }"
        >
            <VAppBarNavIcon
                :class="{ 'd-md-none': !store.alwaysHideSidebar }"
                @click="drawer = !drawer"
            />
            <VAppBarTitle>
                <RouterLink to="/">Voltis</RouterLink>
            </VAppBarTitle>
            <VSpacer />
            <VMenu v-if="qMe.data?.value">
                <template #activator="{ props }">
                    <VBtn v-bind="props" variant="text" class="me-5">
                        {{ qMe.data.value.username }}
                        <VIcon end>mdi-chevron-down</VIcon>
                    </VBtn>
                </template>
                <VList>
                    <VListItem @click="handleLogout" :disabled="mLogout.isPending.value">
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
        <VNavigationDrawer
            v-model="drawer"
            :permanent="!alwaysHideSidebar"
            :temporary="alwaysHideSidebar"
            :style="{
                top: '0',
                height: '100vh',
            }"
        >
            <div class="h-16 flex items-center ms-5!">
                <VAppBarTitle>
                    <RouterLink to="/">Voltis</RouterLink>
                </VAppBarTitle>
            </div>
            <VDivider class="mx-2" />

            <VList v-if="isSettings" nav>
                <VListItem prepend-icon="mdi-arrow-left" @click="router.push('/')">
                    <VListItemTitle>Back</VListItemTitle>
                </VListItem>
                <VDivider class="my-2" />
                <VListItem to="/settings/account" prepend-icon="mdi-account">
                    <VListItemTitle>Account</VListItemTitle>
                </VListItem>
                <template v-if="isAdmin">
                    <VDivider class="my-2" />
                    <VListItem to="/settings/users" prepend-icon="mdi-account-group">
                        <VListItemTitle>Users</VListItemTitle>
                    </VListItem>
                    <VListItem to="/settings/libraries" prepend-icon="mdi-bookshelf">
                        <VListItemTitle>Libraries</VListItemTitle>
                    </VListItem>
                </template>
            </VList>
            <VList v-else nav>
                <VListItem to="/" exact prepend-icon="mdi-home">
                    <VListItemTitle>Home</VListItemTitle>
                </VListItem>
                <VListItem to="/lists" exact prepend-icon="mdi-format-list-bulleted">
                    <VListItemTitle>Lists</VListItemTitle>
                </VListItem>
                <VDivider class="my-2" />
                <VListSubheader>Libraries</VListSubheader>
                <VListItem
                    v-for="library in qLibraries.data?.value"
                    :key="library.id"
                    :to="`/${library.id}`"
                    prepend-icon="mdi-bookshelf"
                >
                    <VListItemTitle>{{ library.name }}</VListItemTitle>
                </VListItem>
                <VDivider class="my-2" />
                <VListItem to="/settings/account" prepend-icon="mdi-cog">
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
import { ref, computed, watch } from 'vue'
import { useDisplay } from 'vuetify'
import { usersApi } from '@/utils/api/users'
import { authApi } from '@/utils/api/auth'
import { librariesApi } from '@/utils/api/libraries'
import { useRouter, useRoute } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import { useLayoutStore } from './useLayoutStore'

const store = useLayoutStore()
const router = useRouter()
const route = useRoute()
const isSettings = computed(() => route.path.startsWith('/settings'))
const { mdAndUp } = useDisplay()
const qMe = usersApi.useMe()
const mLogout = authApi.useLogout()
const qLibraries = librariesApi.useList()
const queryClient = useQueryClient()

const alwaysHideSidebar = computed(() => store.alwaysHideSidebar || !mdAndUp.value)
const drawer = ref(!alwaysHideSidebar.value)

watch(
    () => store.alwaysHideSidebar,
    newVal => {
        if (newVal) {
            drawer.value = false
        }
    }
)

const isAdmin = computed(() => qMe.data.value?.permissions.includes('ADMIN'))

async function handleLogout() {
    await mLogout.mutateAsync()
    queryClient.invalidateQueries({ queryKey: ['users', 'me'] })
    router.push('/auth/login')
}
</script>
