<template>
	<div class="card rounded-none border-t-0 border-x-0 mb-4">
		<div class="flex flex-row items-center gap-2 acontainer">
			<NuxtLink to="/" class="font-bold text-lg font-mono">Voltis</NuxtLink>
			<div class="ml-auto"></div>
			<div>{{ user?.username || 'Not logged in' }}</div>
		</div>
	</div>

	<slot />

	<div class="mb-4"></div>
</template>

<script lang="ts" setup>
import { useUser } from '../state/composables/use-user'

const qUser = useUser()
const user = qUser.data
</script>

<style></style>
