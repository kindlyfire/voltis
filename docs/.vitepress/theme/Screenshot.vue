<script setup lang="ts">
// A screenshot with a light and a dark capture, `<name>.jpg` and `<name>_dark.jpg`. Both are
// rendered and CSS shows the one matching the site theme, so there is no flash on hydration;
// the hidden one is lazy and never fetched.
defineProps<{ name: string; alt: string }>()
</script>

<template>
	<a class="screenshot" :href="`/screenshots/${name}.jpg`" target="_blank">
		<img class="screenshot-light" :src="`/screenshots/${name}.jpg`" :alt="alt" loading="lazy" />
		<img
			class="screenshot-dark"
			:src="`/screenshots/${name}_dark.jpg`"
			:alt="alt"
			loading="lazy"
		/>
	</a>
</template>

<style>
.screenshot {
	display: block;
}
.screenshot-dark,
.dark .screenshot-light {
	display: none;
}
.dark .screenshot-dark {
	display: block;
}
</style>
