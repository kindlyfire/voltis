<template>
    <div v-if="qMe.isLoading.value || qInfo.isLoading.value" class="loading-container">
        <VProgressCircular indeterminate size="64" />
    </div>
    <RouterView v-else />
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { watch } from 'vue'
import { RouterView, useRouter } from 'vue-router'
import { miscApi } from './utils/api/misc'
import { usersApi } from './utils/api/users'

const router = useRouter()
const qMe = usersApi.useMe()
const qInfo = miscApi.useInfo()

watch(
    () =>
        [qMe.data.value, qMe.isLoading.value, router.currentRoute.value, qInfo.data.value] as const,
    ([me, isLoading, route, info]) => {
        if (!isLoading && !me && info) {
            if (info.first_user_flow && route.path !== '/auth/register') {
                router.replace('/auth/register')
                return
            }
            if (!isLoading && !me && !route.path.startsWith('/auth')) {
                router.replace('/auth/login')
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

<style lang="css">
.v-btn {
    text-transform: none;
    letter-spacing: normal;
}

.loading-container {
    display: flex;
    justify-content: center;
    align-items: center;
    height: 100vh;
    width: 100vw;
}

.v-overlay__content > .v-card > .v-card-title {
    padding: 16px 24px 0;
}

body {
    touch-action: manipulation;
}

.v-navigation-drawer__scrim {
    @apply select-none;

    &.fade-transition-leave-active {
        pointer-events: none;
    }
}
</style>
