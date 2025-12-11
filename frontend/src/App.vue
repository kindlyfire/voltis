<template>
	<RouterView />
</template>

<script setup lang="ts">
import { RouterView } from 'vue-router'
import { useTheme } from 'vuetify'
import { onMounted, onUnmounted } from 'vue'

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
</script>

<style lang="css">
.v-btn {
	text-transform: none;
	letter-spacing: normal;
}
</style>
