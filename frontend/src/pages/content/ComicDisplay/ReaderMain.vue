<template>
	<div class="reader-main select-none" @click="controls.handleClick">
		<ReaderModePaged v-if="reader.mode.value === 'paged'" />
		<ReaderModeLongstrip v-else />
	</div>

	<ReaderSidebar @prev="reader.handlePrev" @next="reader.handleNext" />

	<VProgressLinear
		:model-value="reader.progress.value"
		class="reader-progress"
		height="3"
		color="primary"
	/>
</template>

<script setup lang="ts">
import { provide } from 'vue'
import type { PageInfo } from './types'
import { useReader, readerKey } from './use-reader'
import ReaderModePaged from './ReaderModePaged.vue'
import ReaderModeLongstrip from './ReaderModeLongstrip.vue'
import ReaderSidebar from './ReaderSidebar.vue'
import { useReaderControls } from './use-reader-controls'

const props = defineProps<{
	contentId: string
	pages: PageInfo[]
	getPageUrl: (index: number) => string
}>()

const emit = defineEmits<{
	reachStart: []
	reachEnd: []
}>()

const reader = useReader({
	contentId: props.contentId,
	pages: props.pages,
	getPageUrl: props.getPageUrl,
	onReachStart: () => emit('reachStart'),
	onReachEnd: () => emit('reachEnd'),
})

provide(readerKey, reader)

const controls = useReaderControls(reader)
</script>

<style scoped>
.reader-main {
	position: relative;
	width: 100%;
	min-height: calc(100dvh - var(--v-layout-top, 0px));
}

.reader-progress {
	position: fixed;
	bottom: 0 !important;
	top: auto !important;
	left: 0;
	right: 0;
	z-index: 20;
	pointer-events: none;
}
</style>
