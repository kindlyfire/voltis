<template>
    <div class="page-frame">
        <ContentGrid
            :title="library?.name ?? 'Library'"
            :params="{ library_id: libraryId, parent_id: 'null' }"
        />
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import ContentGrid from '@/components/ContentGrid/ContentGrid.vue'
import { librariesApi } from '@/utils/api/libraries'

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
