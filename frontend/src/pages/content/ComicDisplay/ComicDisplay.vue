<template>
	<div class="reader-main select-none" @click="controls.handleClick">
		<ReaderModePaged v-if="reader.settings.mode === 'paged'" />
		<ReaderModeLongstrip v-else />
	</div>

	<ReaderSidebar />

	<VProgressLinear
		:model-value="reader.progress"
		class="reader-progress"
		height="3"
		color="primary"
	/>
</template>

<script setup lang="ts">
import { watch, onUnmounted } from 'vue'
import { useReaderStore, type ReaderStore } from './useComicDisplayStore'
import ReaderModePaged from './ReaderModePaged.vue'
import ReaderModeLongstrip from './ReaderModeLongstrip.vue'
import ReaderSidebar from './ReaderSidebar.vue'
import { useReaderControls } from './useReaderControls'
import { useRouter } from 'vue-router'
import { useNavbarScrollHide } from '@/pages/useLayoutStore'

const props = defineProps<{
	contentId: string
}>()

const emit = defineEmits<{
	reachStart: [reader: ReaderStore]
	reachEnd: [reader: ReaderStore]
	goToSibling: [id: string, fromEnd?: boolean]
}>()

const router = useRouter()
const reader = useReaderStore()
useNavbarScrollHide()

// Set content when props change
watch(
	() => props.contentId,
	() => {
		const _page = router.currentRoute.value.query.page
		const _pageN = parseInt(_page as string)
		const initialPage = _page === 'last' ? 'last' : isNaN(_pageN) ? 0 : _pageN - 1

		reader.setContent({
			contentId: props.contentId,
			initialPage,
		})
	},
	{ immediate: true }
)

onUnmounted(() => {
	reader.dispose()
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
	z-index: 10000;
	pointer-events: none;
}
</style>
