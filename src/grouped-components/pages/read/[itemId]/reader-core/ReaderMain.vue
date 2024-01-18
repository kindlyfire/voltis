<template>
	<div :ref="r => (reader.state.mainRef = (r as HTMLElement))">
		<div class="acontainer" v-if="reader.state.globalError">
			Global error: {{ reader.state.globalError }}
		</div>
		<div
			class="acontainer"
			v-else-if="
				!reader.state.mode || !reader.state.mainRef || !reader.state.scrollRef
			"
		>
			Loading...
		</div>
		<template v-else-if="reader.state.mode">
			<ReaderRenderPages v-if="reader.state.mode === 'pages'" />
			<ReaderRenderLongstrip v-else-if="reader.state.mode === 'longstrip'" />
			<div class="fixed bottom-0 inset-x-0 pointer-events-none p-1 pt-0">
				<UProgress :value="progress" size="sm" />
			</div>
		</template>
		<div class="acontainer" v-else>Error: invalid reading mode</div>
	</div>
	<USlideover
		side="right"
		v-model="reader.state.menuOpen"
		:ui="{
			width: 'max-w-[300px]'
		}"
		:transition="false"
	>
		<ReaderMenu />
	</USlideover>
</template>

<script lang="ts" setup>
import ReaderMenu from './ReaderMenu.vue'
import ReaderRenderLongstrip from './ReaderRenderLongstrip.vue'
import ReaderRenderPages from './ReaderRenderPages.vue'
import { readerKey } from './use-reader'

const reader = inject(readerKey)!

const progress = computed(() => {
	const pages = reader.state.chaptersPages.get(reader.state.chapterId)
	if (!pages || pages.length === 0) return 0
	return ((reader.state.page + 1) / pages.length) * 100
})
</script>

<style></style>
