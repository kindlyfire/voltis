<template>
    <ADialog
        :open="open"
        title="You've completed this series"
        description="Do you want to start again? This will mark all chapters as unread."
        size="sm"
        :dismissible="!mResetReading.isPending.value"
        @update:open="v => !v && close(false)"
    >
        <QueryError :mutation="mResetReading" />
        <template #actions>
            <AButton
                variant="text"
                tone="neutral"
                :disabled="mResetReading.isPending.value"
                @click="close(false)"
            >
                No
            </AButton>
            <AButton :loading="mResetReading.isPending.value" @click="mResetReading.mutate()">
                Yes, start again
            </AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { useMutation } from '@tanstack/vue-query'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import { useToast } from '@/ui/useToast'
import { contentApi } from '@/utils/api/content'

const props = defineProps<{
    open: boolean
    close: (confirmed: boolean) => void
    contentId: string
}>()

const toast = useToast()

const mResetReading = useMutation({
    mutationFn: async () => {
        await contentApi.setSeriesItemStatuses(props.contentId, null)
        await contentApi.updateUserData(props.contentId, { status: 'reading' })
    },
    onSuccess() {
        toast.show({ message: 'Marked all chapters as unread' })
        props.close(true)
    },
})
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './ResetReadingModal.vue'

export function showResetReadingModal(contentId: string): Promise<boolean> {
    return Modals.show<boolean>(Self, { contentId })
}
</script>
