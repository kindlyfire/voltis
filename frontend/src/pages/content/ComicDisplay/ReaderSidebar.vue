<template>
	<VNavigationDrawer
		v-model="reader.sidebarOpen.value"
		temporary
		disable-route-watcher
		location="right"
		width="300"
	>
		<div class="pa-4">
			<div class="d-flex align-center mb-4">
				<span class="text-h6">Reader</span>
				<VSpacer />
				<VBtn icon variant="text" @click="reader.sidebarOpen.value = false">
					<VIcon>mdi-close</VIcon>
				</VBtn>
			</div>

			<div v-if="reader.siblings.value" class="mb-4">
				<div class="text-body-2 text-medium-emphasis mb-2">Chapter</div>
				<div class="d-flex align-center gap-2 mb-2">
					<VBtn
						icon
						size="small"
						variant="tonal"
						:disabled="!reader.prevSibling.value"
						@click="
							reader.prevSibling.value &&
							reader.goToSibling(reader.prevSibling.value.id, true)
						"
					>
						<VIcon>mdi-chevron-left</VIcon>
					</VBtn>
					<VSelect
						:model-value="
							reader.siblings.value.items[reader.siblings.value.currentIndex]?.id
						"
						:items="reader.siblings.value.items"
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
						:disabled="!reader.nextSibling.value"
						@click="
							reader.nextSibling.value &&
							reader.goToSibling(reader.nextSibling.value.id)
						"
					>
						<VIcon>mdi-chevron-right</VIcon>
					</VBtn>
				</div>
				<div class="text-body-2 text-medium-emphasis text-center">
					{{ reader.siblings.value.currentIndex + 1 }} of
					{{ reader.siblings.value.items.length }}
				</div>
			</div>

			<div class="mb-4">
				<div class="text-body-2 text-medium-emphasis mb-1">
					Page {{ reader.currentPage.value + 1 }} of {{ reader.pages.length }}
				</div>
				<VSlider
					:model-value="reader.currentPage.value"
					:min="0"
					:max="reader.pages.length - 1"
					:step="1"
					hide-details
					@update:model-value="reader.goToPage($event)"
				/>
			</div>

			<div class="mb-4">
				<div class="text-body-2 text-medium-emphasis mb-2">Mode</div>
				<VBtnToggle
					:model-value="reader.mode.value"
					mandatory
					variant="outlined"
					divided
					@update:model-value="reader.mode.value = $event"
				>
					<VBtn value="paged">Single Page</VBtn>
					<VBtn value="longstrip">Longstrip</VBtn>
				</VBtnToggle>
			</div>
		</div>
	</VNavigationDrawer>
</template>

<script setup lang="ts">
import { inject } from 'vue'
import { readerKey } from './use-reader'

const reader = inject(readerKey)!
</script>
