<template>
	<VContainer v-if="content.isLoading.value">
		<VProgressCircular indeterminate />
	</VContainer>
	<VContainer v-else-if="content.error.value">
		<VAlert type="error">{{ content.error.value.message }}</VAlert>
	</VContainer>
	<template v-else-if="content.data.value">
		<ComicDisplay v-if="content.data.value.type === 'comic'" :content="content.data.value" />
		<ComicSeriesDisplay
			v-else-if="content.data.value.type === 'comic_series'"
			:content="content.data.value"
		/>
		<BookDisplay v-else-if="content.data.value.type === 'book'" :content="content.data.value" />
		<BookSeriesDisplay
			v-else-if="content.data.value.type === 'book_series'"
			:content="content.data.value"
		/>
	</template>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { contentApi } from '@/utils/api/content'
import ComicDisplay from './ComicDisplay.vue'
import ComicSeriesDisplay from './ComicSeriesDisplay.vue'
import BookDisplay from './BookDisplay.vue'
import BookSeriesDisplay from './BookSeriesDisplay.vue'

const route = useRoute()
const contentId = computed(() => route.params.id as string)

const content = contentApi.useGet(contentId)
</script>
