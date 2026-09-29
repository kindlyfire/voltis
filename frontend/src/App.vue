<template>
    <TooltipProvider :delay-duration="400">
        <div
            v-if="qMe.isLoading.value || qInfo.isLoading.value"
            class="flex h-screen w-screen items-center justify-center"
        >
            <ASpinner size="lg" />
        </div>
        <RouterView v-else />
    </TooltipProvider>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { TooltipProvider } from 'reka-ui'
import { watch } from 'vue'
import { RouterView, START_LOCATION, useRouter } from 'vue-router'
import { useLayoutStore } from './pages/_layout/useLayoutStore'
import { useScanSync } from './stores/scans'
import ASpinner from './ui/ASpinner.vue'
import { miscApi } from './utils/api/misc'
import { usersApi } from './utils/api/users'
import { redirectQuery } from './utils/redirect'
import { useSubmitShortcut } from './utils/submitShortcut'

// Created here so the theme applies to every page, the auth pages included.
useLayoutStore()

const router = useRouter()
const qMe = usersApi.useMe()
const qInfo = miscApi.useInfo()

useScanSync()
useSubmitShortcut()

watch(
    () =>
        [qMe.data.value, qMe.isLoading.value, router.currentRoute.value, qInfo.data.value] as const,
    ([me, isLoading, route, info]) => {
        // Wait for the initial navigation, or the redirect below would record `/`.
        if (route === START_LOCATION) return
        if (!isLoading && !me && info) {
            // The sign-in is verified but unfinished: it owns this page.
            if (route.path === '/auth/oidc/complete') {
                return
            }
            if (import.meta.env.DEV && route.path === '/_kit') {
                return
            }
            if (info.first_user_flow && route.path !== '/auth/register') {
                router.replace('/auth/register')
                return
            }
            if (!['login', 'register'].includes(route.name as string)) {
                router.replace({
                    path: '/auth/login',
                    query: redirectQuery(route.fullPath),
                })
            }
        }
    },
    { immediate: true }
)

useHead({
    titleTemplate(title) {
        return title ? `${title} • Voltis` : 'Voltis'
    },
})
</script>
