<template>
	<VContainer>
		<h1 class="text-h4 mb-4">{{ library?.name }}</h1>
		<div :class="LIBRARY_GRID_CLASSES">
			<RouterLink
				v-for="item in contents.data?.value"
				:key="item.id"
				:to="`/${item.id}`"
				class="block"
				:title="item.title"
			>
				<VCard class="relative">
					<VImg
						v-if="item.cover_uri"
						:src="`${API_URL}/files/cover/${item.id}?v=${item.file_mtime}`"
						:aspect-ratio="2 / 3"
						cover
					/>
					<span
						v-if="item.children_count"
						class="absolute top-2 right-2 bg-black/70 text-white text-xs font-medium px-2 py-0.5 rounded-full"
					>
						{{ item.children_count }}
					</span>
					<VCardTitle class="text-body-2">{{ item.title }}</VCardTitle>
				</VCard>
			</RouterLink>
		</div>
	</VContainer>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { contentApi } from '@/utils/api/content'
import { librariesApi } from '@/utils/api/libraries'
import { API_URL } from '@/utils/fetch'
import { useHead } from '@unhead/vue'
import { LIBRARY_GRID_CLASSES } from '@/utils/misc'

const route = useRoute()
const libraryId = computed(() => route.params.id as string)

const libraries = librariesApi.useList()
const library = computed(() => libraries.data?.value?.find(l => l.id === libraryId.value))

const contents = contentApi.useList(
	computed(() => ({ library_id: libraryId.value, parent_id: 'null' }))
)

useHead({
	title() {
		return library.value?.name ?? 'Library'
	},
})
</script>
