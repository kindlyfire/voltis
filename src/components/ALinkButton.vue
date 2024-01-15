<template>
	<UButton
		v-bind="$attrs"
		:to="props.to"
		:variant="isActive ? 'soft' : 'ghost'"
		:color="isActive ? 'primary' : 'gray'"
	>
		<template v-for="(_, slot) of $slots" v-slot:[slot]="scope"
			><slot :name="slot" v-bind="scope"
		/></template>
	</UButton>
</template>

<script lang="ts" setup>
import type { UButton } from '../../.nuxt/components'

const props = defineProps<{
	to: string
}>()

const route = useRoute()
const isActive = computed(() => route.path === props.to)
</script>

<style></style>
