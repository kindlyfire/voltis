<template>
	<VContainer>
		<h1 class="text-h4 mb-4">{{ library?.name }}</h1>
		<div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-8 gap-4">
			<RouterLink
				v-for="item in contents.data?.value"
				:key="item.id"
				:to="`/${item.id}`"
				class="block"
			>
				<VCard>
					<VImg
						v-if="item.cover_uri"
						:src="`${API_URL}/files/cover/${item.id}`"
						:aspect-ratio="2 / 3"
						cover
					/>
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

const route = useRoute()
const libraryId = computed(() => route.params.id as string)

const libraries = librariesApi.useList()
const library = computed(() => libraries.data?.value?.find(l => l.id === libraryId.value))

const contents = contentApi.useList(
	computed(() => ({ library_id: libraryId.value, parent_id: 'null' }))
)
</script>
