<template>
    <VApp>
        <VAppBar
            :flat="store.navbarHidden.value"
            :style="store.navbarHidden.value && { transform: 'translateY(-64px)' }"
        >
            <VAppBarNavIcon @click="store.setSidebarOpen(!store.sidebarOpen.value)" />
            <VAppBarTitle>
                <RouterLink to="/">Voltis</RouterLink>
            </VAppBarTitle>
            <VSpacer />
            <VBtn
                ref="themeBtnRef"
                variant="text"
                size="small"
                :icon="store.theme === 'light' ? 'mdi-weather-sunny' : 'mdi-weather-night'"
                title="Toggle theme (Shift+click or long press to reset to system)"
            />
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
            :model-value="store.sidebarOpen.value"
            :permanent="!store.sidebarTemporary.value"
            :temporary="store.sidebarTemporary.value"
            @update:model-value="val => store.setSidebarOpen(val)"
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
        <VMain :style="store.navbarTemporary ? { '--v-layout-top': '0px' } : {}">
            <RouterView />
        </VMain>
        <ModalContainer />
    </VApp>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ModalContainer } from '@/utils/modals'
import { usersApi } from '@/utils/api/users'
import { authApi } from '@/utils/api/auth'
import { librariesApi } from '@/utils/api/libraries'
import { useRouter, useRoute } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import { onLongPress, useEventListener } from '@vueuse/core'
import { useLayoutStore } from './useLayoutStore'

const store = useLayoutStore()

const router = useRouter()
const route = useRoute()
const isSettings = computed(() => route.path.startsWith('/settings'))
const qMe = usersApi.useMe()
const mLogout = authApi.useLogout()
const qLibraries = librariesApi.useList()
const queryClient = useQueryClient()

const isAdmin = computed(() => qMe.data.value?.permissions.includes('ADMIN'))

async function handleLogout() {
    await mLogout.mutateAsync()
    queryClient.invalidateQueries({ queryKey: ['users', 'me'] })
    router.push('/auth/login')
}

/** We out here overcomplicating things to the bone. It's nice, though. */
const themeBtnRef = ref<HTMLElement | null>(null)
let longPressTriggered = false
onLongPress(
    themeBtnRef,
    () => {
        longPressTriggered = true
        store.resetTheme()
    },
    { delay: 500 }
)
useEventListener(document.body, 'pointerup', () =>
    setTimeout(() => (longPressTriggered = false), 0)
)
useEventListener(themeBtnRef, 'click', e => {
    if (longPressTriggered) return
    e.shiftKey ? store.resetTheme() : store.toggleTheme()
})
</script>
