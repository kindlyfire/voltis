<template>
	<div v-if="qMe.isLoading.value" class="loading-container">
		<VProgressCircular indeterminate size="64" />
	</div>
	<RouterView v-else />
</template>

<script setup lang="ts">
import { RouterView, useRouter } from 'vue-router'
import { useTheme } from 'vuetify'
import { onMounted, onUnmounted, watch } from 'vue'
import { usersApi } from './utils/api/users'
import { useHead } from '@unhead/vue'

const theme = useTheme()

const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
const updateTheme = (e: MediaQueryListEvent | MediaQueryList) => {
	theme.global.name.value = e.matches ? 'dark' : 'light'
}

onMounted(() => {
	mediaQuery.addEventListener('change', updateTheme)
})

onUnmounted(() => {
	mediaQuery.removeEventListener('change', updateTheme)
})

const router = useRouter()
const qMe = usersApi.useMe()

watch(
	() => [qMe.data.value, qMe.isLoading.value, router.currentRoute.value] as const,
	([me, isLoading, route]) => {
		if (!isLoading && !me && !route.path.startsWith('/auth')) {
			router.replace('/auth/login')
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
</style>
