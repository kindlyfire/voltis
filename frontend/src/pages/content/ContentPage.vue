<template>
	<VContainer v-if="qContent.isLoading.value || qSiblings.isLoading.value">
		<VProgressCircular indeterminate />
	</VContainer>
	<VContainer v-else-if="qContent.error.value || qSiblings.error.value">
		<VAlert type="error">{{
			qContent.error.value?.message || qSiblings.error.value?.message
		}}</VAlert>
	</VContainer>
	<template v-else-if="qContent.data.value && qSiblings.data.value">
		<ComicDisplay
			v-if="qContent.data.value.type === 'comic'"
			:content="qContent.data.value"
			:siblings="qSiblings.data.value"
		/>
		<ComicSeriesDisplay
			v-else-if="qContent.data.value.type === 'comic_series'"
			:content="qContent.data.value"
		/>
		<BookDisplay
			v-else-if="qContent.data.value.type === 'book'"
			:content="qContent.data.value"
		/>
		<BookSeriesDisplay
			v-else-if="qContent.data.value.type === 'book_series'"
			:content="qContent.data.value"
		/>
	</template>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { contentApi } from '@/utils/api/content'
import ComicDisplay from './ComicDisplay/ComicDisplay.vue'
import ComicSeriesDisplay from './ComicSeriesDisplay.vue'
import BookDisplay from './BookDisplay.vue'
import BookSeriesDisplay from './BookSeriesDisplay.vue'
import { useHead } from '@unhead/vue'

const route = useRoute()
const contentId = computed(() => route.params.id as string)

const qContent = contentApi.useGet(contentId)
const qSiblings = contentApi.useList(
	computed(() =>
		qContent.data.value?.parent_id ? { parent_id: qContent.data.value.parent_id } : {}
	)
)

useHead({
	title() {
		return qContent.data.value?.title ?? null
	},
})
</script>
