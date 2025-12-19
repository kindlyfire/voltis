<template>
	<VNavigationDrawer
		v-model="reader.sidebarOpen"
		temporary
		disable-route-watcher
		location="right"
		width="300"
	>
		<div class="pa-4">
			<div class="d-flex align-center mb-4">
				<span class="text-h6">Reader</span>
				<VSpacer />
				<VBtn icon variant="text" @click="reader.sidebarOpen = false">
					<VIcon>mdi-close</VIcon>
				</VBtn>
			</div>

			<div v-if="reader.siblings" class="mb-4">
				<div class="text-body-2 text-medium-emphasis mb-2">Chapter</div>
				<div class="d-flex align-center gap-2 mb-2">
					<VBtn
						icon
						size="small"
						variant="tonal"
						:disabled="!reader.prevSibling"
						@click="
							reader.prevSibling && reader.goToSibling(reader.prevSibling.id, true)
						"
					>
						<VIcon>mdi-chevron-left</VIcon>
					</VBtn>
					<VSelect
						:model-value="reader.siblings.items[reader.siblings.currentIndex]?.id"
						:items="reader.siblings.items"
						item-title="title"
						item-value="id"
						density="compact"
						hide-details
						class="grow"
						@update:model-value="reader.goToSibling($event)"
					/>
					<VBtn
						icon
						size="small"
						variant="tonal"
						:disabled="!reader.nextSibling"
						@click="reader.nextSibling && reader.goToSibling(reader.nextSibling.id)"
					>
						<VIcon>mdi-chevron-right</VIcon>
					</VBtn>
				</div>
				<div class="text-body-2 text-medium-emphasis text-center">
					{{ reader.siblings.currentIndex + 1 }} of
					{{ reader.siblings.items.length }}
				</div>
			</div>

			<div class="mb-4">
				<div class="text-body-2 text-medium-emphasis mb-1">
					Page {{ reader.currentPage + 1 }} of {{ reader.pages.length }}
				</div>
				<VSlider
					:model-value="reader.currentPage"
					:min="0"
					:max="reader.pages.length - 1"
					:step="1"
					hide-details
					@update:model-value="reader.goToPage($event)"
				/>
			</div>

			<div class="mb-4">
				<div class="text-body-2 text-medium-emphasis mb-2">Mode</div>
				<VBtnToggle v-model="reader.mode" mandatory variant="outlined" divided>
					<VBtn value="paged">Single Page</VBtn>
					<VBtn value="longstrip">Longstrip</VBtn>
				</VBtnToggle>
			</div>
		</div>
	</VNavigationDrawer>
</template>

<script setup lang="ts">
import { useReaderStore } from './use-reader-store'

const reader = useReaderStore()
</script>
