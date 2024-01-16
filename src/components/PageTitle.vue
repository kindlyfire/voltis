<template>
	<div class="text-xl md:text-5xl font-bold" v-if="title">{{ title }}</div>
</template>

<script lang="ts" setup>
const props = defineProps<{
	title?: string
	pagetitle?: string
}>()
const emit = defineEmits<{}>()

const runtimeConfig = useRuntimeConfig()
useSeoMeta({
	title() {
		const t = props.pagetitle || props.title
		if (!t) return runtimeConfig.public.brand
		return t + ' - ' + runtimeConfig.public.brand
	}
})
</script>

<style></style>
