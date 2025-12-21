<template>
	<div class="reader-paged d-flex align-center justify-center">
		<template v-if="loader">
			<div v-if="loader.error.value" class="d-flex flex-column align-center gap-2">
				<div class="text-error">{{ loader.error.value }}</div>
				<VBtn @click="loader.load()">Retry</VBtn>
			</div>
			<div
				v-else-if="loader.loading.value || !loader.blobUrl.value"
				class="d-flex align-center justify-center"
			>
				<VProgressCircular indeterminate size="64" />
			</div>
			<img
				v-else
				:src="loader.blobUrl.value"
				class="reader-paged__image"
				:style="imageStyle"
			/>
		</template>
	</div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useReaderStore } from './useComicDisplayStore'

const reader = useReaderStore()

const loader = computed(() => reader.getLoader(reader.currentPage))

const pageInfo = computed(() => reader.pages[reader.currentPage])

const imageStyle = computed(() => {
	if (!pageInfo.value) return {}
	return {
		aspectRatio: `${pageInfo.value.width} / ${pageInfo.value.height}`,
	}
})
</script>

<style scoped>
.reader-paged {
	width: 100%;
	height: 100%;
	min-height: calc(100dvh - var(--v-layout-top, 0px));
}

.reader-paged__image {
	max-width: 100%;
	max-height: calc(100dvh - var(--v-layout-top, 0px));
	object-fit: contain;
}
</style>
