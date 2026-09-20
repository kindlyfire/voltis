<template>
    <VContainer>
        <h2 class="text-h5 mb-4">Contents</h2>
        <BookContents
            v-if="qStructure.data.value"
            :content-id="content.id"
            :structure="qStructure.data.value"
        />
        <div v-else-if="qStructure.isLoading.value" class="flex justify-center py-8">
            <VProgressCircular indeterminate />
        </div>
        <AQueryError v-else-if="qStructure.error.value" :query="qStructure" />
    </VContainer>
</template>

<script setup lang="ts">
import AQueryError from '@/components/AQueryError.vue'
import BookContents from '@/pages/read/BookDisplay/BookContents.vue'
import { contentApi } from '@/utils/api/content'
import type { Content } from '@/utils/api/types'

const props = defineProps<{
    content: Content
}>()

const qStructure = contentApi.useBookStructure(() => props.content.id)
</script>
