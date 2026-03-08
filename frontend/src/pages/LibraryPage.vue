<template>
    <VContainer>
        <h1 class="text-h5 mb-3">{{ library?.name }}</h1>
        <AContentGrid :params="{ library_id: libraryId, parent_id: 'null' }" />
    </VContainer>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { librariesApi } from '@/utils/api/libraries'
import { useHead } from '@unhead/vue'
import AContentGrid from '@/components/AContentGrid/AContentGrid.vue'

const route = useRoute()
const libraryId = computed(() => route.params.id as string)

const qLibraries = librariesApi.useList()
const library = computed(() => qLibraries.data?.value?.find(l => l.id === libraryId.value))

useHead({
    title() {
        return library.value?.name ?? 'Library'
    },
})
</script>
