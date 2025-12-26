<template>
	<VContainer v-if="qContent.error.value">
		<VAlert type="error">{{ qContent.error.value?.message }}</VAlert>
	</VContainer>
	<div v-else-if="!contentType" class="absolute inset-0 flex items-center justify-center">
		<VProgressCircular indeterminate size="64" />
	</div>
	<template v-else>
		<ComicDisplay v-if="contentType === 'comic'" :contentId="contentId" />
		<ComicSeriesDisplay v-else-if="contentType === 'comic_series'" :content-id="contentId" />
		<BookDisplay v-else-if="contentType === 'book'" :content-id="contentId" />
		<BookSeriesDisplay v-else-if="contentType === 'book_series'" :content-id="contentId" />
	</template>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { contentApi } from '@/utils/api/content'
import ComicDisplay from './ComicDisplay/ComicDisplay.vue'
import ComicSeriesDisplay from './ComicSeriesDisplay.vue'
import BookDisplay from './BookDisplay/BookDisplay.vue'
import BookSeriesDisplay from './BookSeriesDisplay.vue'
import { useHead } from '@unhead/vue'
import type { ContentType } from '@/utils/api/types'

const route = useRoute()
const contentId = computed(() => route.params.id as string)
const qContent = contentApi.useGet(contentId)

// We cache the content type to avoid flickering when navigating between
// contents.
const contentType = ref(null as null | ContentType)
watch(
	() => qContent.data.value,
	newContent => {
		if (newContent) {
			contentType.value = newContent.type
		}
	},
	{ immediate: true }
)

useHead({
	title() {
		return qContent.data.value?.title ?? null
	},
})
</script>
