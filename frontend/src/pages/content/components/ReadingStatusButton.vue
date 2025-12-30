<template>
    <VSelect
        :model-value="currentStatus"
        :items="statusOptions"
        :loading="qContent.isLoading.value || mUpdateUserData.isPending.value"
        :placeholder="'Set status'"
        density="comfortable"
        variant="solo"
        hide-details
        clearable
        @update:model-value="updateStatus"
        class="sm:max-w-60 grow!"
    />
</template>

<script setup lang="ts">
import { contentApi } from '@/utils/api/content'
import type { ReadingStatus } from '@/utils/api/types'
import { computed } from 'vue'

const props = defineProps<{
    contentId: string | null | undefined
}>()

const qContent = contentApi.useGet(() => props.contentId)
const content = qContent.data

const statusOptions: { value: ReadingStatus; title: string }[] = [
    { value: 'reading', title: 'Reading' },
    { value: 'completed', title: 'Completed' },
    { value: 'on_hold', title: 'On Hold' },
    { value: 'dropped', title: 'Dropped' },
    { value: 'plan_to_read', title: 'Plan to Read' },
]

const currentStatus = computed(() => content.value?.user_data?.status ?? null)
const mUpdateUserData = contentApi.useUpdateUserData()

async function updateStatus(status: ReadingStatus | null) {
    if (!content.value) return
    await mUpdateUserData.mutateAsync({ contentId: content.value.id, status })
    await qContent.refetch()
}
</script>
