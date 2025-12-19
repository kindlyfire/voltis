<template>
	<div class="reader-main select-none" @click="controls.handleClick">
		<ReaderModePaged v-if="reader.mode === 'paged'" />
		<ReaderModeLongstrip v-else />
	</div>

	<ReaderSidebar @prev="reader.handlePrev" @next="reader.handleNext" />

	<VProgressLinear
		:model-value="reader.progress"
		class="reader-progress"
		height="3"
		color="primary"
	/>
</template>

<script setup lang="ts">
import { watch, onUnmounted } from 'vue'
import type { PageInfo, SiblingsInfo } from './types'
import { useReaderStore } from './use-reader-store'
import ReaderModePaged from './ReaderModePaged.vue'
import ReaderModeLongstrip from './ReaderModeLongstrip.vue'
import ReaderSidebar from './ReaderSidebar.vue'
import { useReaderControls } from './use-reader-controls'

const props = defineProps<{
	contentId: string
	pages: PageInfo[]
	siblings?: SiblingsInfo | null
	getPageUrl: (index: number) => string
}>()

const emit = defineEmits<{
	reachStart: []
	reachEnd: []
	goToSibling: [id: string, fromEnd?: boolean]
}>()

const reader = useReaderStore()

// Set content when props change
watch(
	() => props.contentId,
	() => {
		reader.setContent({
			contentId: props.contentId,
			pages: props.pages,
			siblings: props.siblings,
			getPageUrl: props.getPageUrl,
			onReachStart: () => emit('reachStart'),
			onReachEnd: () => emit('reachEnd'),
			onGoToSibling: (id, fromEnd) => emit('goToSibling', id, fromEnd),
		})
	},
	{ immediate: true }
)

onUnmounted(() => {
	reader.disposeLoaders()
})

const controls = useReaderControls()
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
