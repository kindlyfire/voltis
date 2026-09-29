<template>
    <TooltipProvider :delay-duration="400">
        <div
            v-if="qMe.isLoading.value || qInfo.isLoading.value"
            id="app-loading"
            class="flex h-screen w-screen items-center justify-center"
        >
            <ASpinner size="lg" />
        </div>
        <div v-else-if="failed" class="flex min-h-screen items-center justify-center p-4">
            <div class="flex w-full max-w-md flex-col items-start gap-3">
                <AAlert tone="danger" title="Voltis couldn't load" class="w-full">
                    {{ RequestError.getMessage(failed.error.value) }}
                </AAlert>
                <AButton @click="failed.refetch()">Retry</AButton>
            </div>
        </div>
        <RouterView v-else />
    </TooltipProvider>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { TooltipProvider } from 'reka-ui'
import { computed, watch } from 'vue'
import { RouterView, START_LOCATION, useRouter } from 'vue-router'
import { useLayoutStore } from './pages/_layout/useLayoutStore'
import { useScanSync } from './stores/scans'
import AAlert from './ui/AAlert.vue'
import AButton from './ui/AButton.vue'
import ASpinner from './ui/ASpinner.vue'
import { miscApi } from './utils/api/misc'
import { usersApi } from './utils/api/users'
import { RequestError } from './utils/fetch'
import { redirectQuery } from './utils/redirect'
import { useSubmitShortcut } from './utils/submitShortcut'

// Created here so the theme applies to every page, the auth pages included.
useLayoutStore()

const router = useRouter()
const qMe = usersApi.useMe()
const qInfo = miscApi.useInfo()
// Only a failure with nothing loaded: the app can't start without these.
const failed = computed(() => [qMe, qInfo].find(q => q.isError.value && q.data.value === undefined))

useScanSync()
useSubmitShortcut()

watch(
    () =>
        [qMe.data.value, qMe.isLoading.value, router.currentRoute.value, qInfo.data.value] as const,
    ([me, isLoading, route, info]) => {
        // Wait for the initial navigation, or the redirect below would record `/`.
        if (route === START_LOCATION) return
        // `null` is signed out; `undefined` is an error, shown above.
        if (!isLoading && me === null && info) {
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
