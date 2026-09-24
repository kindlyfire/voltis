<template>
    <ASelect
        :model-value="currentStatus"
        :options="readingStatusOptions"
        label="Reading status"
        placeholder="Set status"
        size="sm"
        clearable
        :loading="qContent.isLoading.value || mUpdateUserData.isPending.value"
        @update:model-value="updateStatus"
    />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import ASelect from '@/ui/ASelect.vue'
import { useToast } from '@/ui/useToast'
import { contentApi } from '@/utils/api/content'
import { READING_STATUS_LABELS, type ReadingStatus } from '@/utils/api/types'
import { readingStatusOptions } from '@/utils/misc'

const props = defineProps<{
    contentId: string | null | undefined
}>()

const qContent = contentApi.useGet(() => props.contentId)
const content = qContent.data

const currentStatus = computed(() => content.value?.user_data?.status ?? null)
const mUpdateUserData = contentApi.useUpdateUserData()
const toast = useToast()

async function updateStatus(status: ReadingStatus | null) {
    if (!content.value) return
    try {
        await mUpdateUserData.mutateAsync({ contentId: content.value.id, status })
    } catch {
        toast.show({ message: 'Could not update the reading status', tone: 'danger' })
        return
    }
    toast.show({
        message: status
            ? `Marked as ${READING_STATUS_LABELS[status]}`
            : 'Cleared the reading status',
    })
}
</script>
