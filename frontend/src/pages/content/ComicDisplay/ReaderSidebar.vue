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
