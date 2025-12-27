<template>
	<div class="reader-paged d-flex align-center justify-center">
		<template v-if="loader">
			<div v-if="loader.error" class="d-flex flex-column align-center gap-2">
				<div class="text-error">{{ loader.error }}</div>
				<VBtn @click="loader.load()">Retry</VBtn>
			</div>
			<div
				v-else-if="loader.loading || !loader.blobUrl"
				class="d-flex align-center justify-center"
			>
				<VProgressCircular indeterminate size="64" />
			</div>
			<img v-else :src="loader.blobUrl" class="reader-paged__image" />
		</template>
	</div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useReaderStore } from './useComicDisplayStore'

const reader = useReaderStore()

const loader = computed(() => reader.state?.loaders[reader.state.page])
</script>

<style scoped>
.reader-paged {
	width: 100%;
	height: 100%;
	min-height: calc(100dvh - var(--v-layout-top, 0px));
}

.reader-paged__image {
	width: 100%;
	height: calc(100dvh - var(--v-layout-top, 0px));
	object-fit: contain;
}
</style>
