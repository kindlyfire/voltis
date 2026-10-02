<template>
    <div class="flex flex-col items-start gap-1">
        <ASelect
            :model-value="currentStatus"
            :options="readingStatusOptions"
            label="Reading status"
            placeholder="Set status"
            size="sm"
            clearable
            :loading="qContent.isLoading.value || mCommand.isPending.value"
            class="w-full"
            @update:model-value="updateStatus"
        />
        <ATooltip v-if="completeHint" :text="completeHint">
            <button
                type="button"
                class="a-focus text-primary rounded-sm text-[13px] font-medium hover:underline"
                :disabled="mCommand.isPending.value"
                @click="updateStatus('completed')"
            >
                Mark completed
            </button>
        </ATooltip>
    </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import ASelect from '@/ui/ASelect.vue'
import ATooltip from '@/ui/ATooltip.vue'
import { useToast } from '@/ui/useToast'
import { contentApi } from '@/utils/api/content'
import { readingApi } from '@/utils/api/reading'
import { READING_STATUS_LABELS, type ReadingStatus } from '@/utils/api/types'
import { childNoun } from '@/utils/contentProgress'
import { readingStatusOptions } from '@/utils/misc'
import { showMarkSeriesCompletedModal } from './MarkSeriesCompletedModal.vue'

const props = defineProps<{
    contentId: string | null | undefined
}>()

const qContent = contentApi.useGet(() => props.contentId)
const content = qContent.data

const currentStatus = computed(() => content.value?.user_data?.status ?? null)

// A caught-up series that isn't completed yet: offer to complete it, and say why.
const completeHint = computed(() => {
    const c = content.value
    const n = c?.children_count ?? 0
    if (
        !c?.type.endsWith('_series') ||
        !n ||
        c.unread_children_count ||
        currentStatus.value === 'completed'
    ) {
        return null
    }
    const ended = ['completed', 'cancelled'].includes(c.meta?.status ?? '')
    return `All ${n} ${childNoun(c.type, n)} in your library are read${ended ? ', and the series has ended' : ''}.`
})
const mCommand = readingApi.useCommand()
const toast = useToast()

async function updateStatus(status: ReadingStatus | null) {
    const c = content.value
    if (!c) return
    // Completing a series with unread volumes asks whether they're read too.
    if (status === 'completed' && c.type.includes('series') && c.unread_children_count) {
        await showMarkSeriesCompletedModal(c.id)
        return
    }
    try {
        await mCommand.mutateAsync({ contentId: c.id, request: { op: 'set_status', status } })
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
