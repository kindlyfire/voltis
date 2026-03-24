<template>
    <VSelect
        :model-value="currentStatus"
        :items="readingStatusOptions"
        :loading="qContent.isLoading.value || mUpdateUserData.isPending.value"
        :placeholder="'Set status'"
        density="comfortable"
        variant="solo"
        hide-details
        clearable
        @update:model-value="updateStatus"
        class="grow! sm:max-w-60"
    />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { contentApi } from '@/utils/api/content'
import type { ReadingStatus } from '@/utils/api/types'
import { readingStatusOptions } from '@/utils/misc'

const props = defineProps<{
    contentId: string | null | undefined
}>()

const qContent = contentApi.useGet(() => props.contentId)
const content = qContent.data

const currentStatus = computed(() => content.value?.user_data?.status ?? null)
const mUpdateUserData = contentApi.useUpdateUserData()

async function updateStatus(status: ReadingStatus | null) {
    if (!content.value) return
    await mUpdateUserData.mutateAsync({ contentId: content.value.id, status })
    await qContent.refetch()
}
</script>
