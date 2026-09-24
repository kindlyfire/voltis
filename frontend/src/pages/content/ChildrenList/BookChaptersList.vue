<template>
    <section class="flex max-w-[780px] flex-col gap-3.5" aria-labelledby="contents-heading">
        <h2 id="contents-heading" class="font-display text-[30px] font-semibold">Contents</h2>
        <ACard v-if="qStructure.data.value" padding="md">
            <BookContents :content-id="content.id" :structure="qStructure.data.value" />
        </ACard>
        <div v-else-if="qStructure.isLoading.value" class="flex justify-center py-8">
            <ASpinner />
        </div>
        <QueryError v-else-if="qStructure.error.value" :query="qStructure" />
    </section>
</template>

<script setup lang="ts">
import QueryError from '@/components/QueryError.vue'
import BookContents from '@/pages/read/BookDisplay/BookContents.vue'
import ACard from '@/ui/ACard.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { contentApi } from '@/utils/api/content'
import type { Content } from '@/utils/api/types'

const props = defineProps<{
    content: Content
}>()

const qStructure = contentApi.useBookStructure(() => props.content.id)
</script>
