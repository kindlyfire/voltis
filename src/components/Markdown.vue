<template>
	<div class="Markdown" v-html="qHtml.data.value ?? ''"></div>
</template>

<script lang="ts" setup>
import { useQuery } from '@tanstack/vue-query'
import DOMPurify from 'dompurify'
import { marked } from 'marked'

const props = defineProps<{
	text: string
}>()
const emit = defineEmits<{}>()

const qHtml = useQuery({
	queryKey: ['html', toRef(props, 'text')],
	async queryFn() {
		return DOMPurify.sanitize(await marked(props.text))
	}
})
</script>

<style>
.Markdown a {
	@apply text-primary hover:underline;
}
</style>
