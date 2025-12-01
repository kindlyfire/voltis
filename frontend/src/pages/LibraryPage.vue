<template>
	<VContainer>
		<h1 class="text-h4 mb-4">{{ library?.name }}</h1>
		<VRow>
			<VCol v-for="item in contents.data?.value" :key="item.id" cols="6" sm="4" md="3" lg="2">
				<VCard>
					<VImg
						v-if="item.cover_uri"
						:src="`/api/files/cover/${item.id}`"
						:aspect-ratio="2 / 3"
						cover
					/>
					<VCardTitle class="text-body-2">{{ item.title }}</VCardTitle>
				</VCard>
			</VCol>
		</VRow>
	</VContainer>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { contentApi } from '@/utils/api/content'
import { librariesApi } from '@/utils/api/libraries'

const route = useRoute()
const libraryId = computed(() => route.params.id as string)

const libraries = librariesApi.useList()
const library = computed(() => libraries.data?.value?.find(l => l.id === libraryId.value))

const contents = contentApi.useList(computed(() => ({ library_id: libraryId.value })))
</script>
