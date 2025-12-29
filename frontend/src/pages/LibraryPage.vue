<template>
    <VContainer>
        <h1 class="text-h4 mb-4">{{ library?.name }}</h1>
        <AContentGrid
            :items="qContents.data.value?.data ?? []"
            :loading="qContents.isLoading.value"
        />
    </VContainer>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { contentApi } from '@/utils/api/content'
import { librariesApi } from '@/utils/api/libraries'
import { useHead } from '@unhead/vue'
import AContentGrid from '@/components/AContentGrid.vue'

const route = useRoute()
const libraryId = computed(() => route.params.id as string)

const qLibraries = librariesApi.useList()
const library = computed(() => qLibraries.data?.value?.find(l => l.id === libraryId.value))

const qContents = contentApi.useList(
    computed(() => ({ library_id: libraryId.value, parent_id: 'null' }))
)

useHead({
    title() {
        return library.value?.name ?? 'Library'
    },
})
</script>
