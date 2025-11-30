<template>
	<VAlert v-if="mutation.isError.value" type="error" variant="tonal" closable>
		{{ errorMessage }}
	</VAlert>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { UseMutationReturnType } from '@tanstack/vue-query'
import { RequestError } from '@/utils/fetch'

const props = defineProps<{
	mutation: UseMutationReturnType<any, Error, any, any>
}>()

const errorMessage = computed(() => {
	const error = props.mutation.error.value
	if (!error) return ''
	return RequestError.getMessage(error)
})
</script>
