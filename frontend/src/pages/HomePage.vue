<template>
	<VContainer>
		<h1 class="text-xl">Hello, World!</h1>
	</VContainer>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'

useHead({
	title: 'Home',
})
</script>
