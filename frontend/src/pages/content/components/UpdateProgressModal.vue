<template>
    <ADialog
        :open="open"
        title="Update progress"
        :dismissible="!mUpdate.isPending.value"
        @update:open="v => !v && close()"
    >
        <div class="flex flex-col gap-4">
            <ARadioGroup v-model="action" :options="actionOptions" label="Action" hide-label />

            <template v-if="action === 'mark_until'">
                <div v-if="qChildren.isLoading.value" class="flex justify-center py-4">
                    <ASpinner />
                </div>
                <QueryError :query="qChildren" />
                <ACombobox
                    v-if="qChildren.isSuccess.value"
                    v-model="selectedChildId"
                    :options="childOptions"
                    label="Chapter"
                    placeholder="Select a chapter"
                    hint="Everything up to and including the selected chapter will be marked as completed, and chapters after will be marked unread."
                />
            </template>

            <QueryError :mutation="mUpdate" />
        </div>

        <template #actions>
            <AButton
                variant="text"
                tone="neutral"
                :disabled="mUpdate.isPending.value"
                @click="close()"
            >
                Cancel
            </AButton>
            <AButton
                :loading="mUpdate.isPending.value"
                :disabled="!action || (action === 'mark_until' && !selectedChildId)"
                @click="mUpdate.mutate()"
            >
                Confirm
            </AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ACombobox from '@/ui/ACombobox.vue'
import ADialog from '@/ui/ADialog.vue'
import ARadioGroup from '@/ui/ARadioGroup.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { useToast } from '@/ui/useToast'
import { contentApi } from '@/utils/api/content'

const props = defineProps<{
    open: boolean
    close: () => void
    contentId: string
}>()

type Action = 'reset' | 'mark_all' | 'mark_until'

const actionOptions = [
    { value: 'reset', label: 'Reset progress' },
    { value: 'mark_all', label: 'Mark all as read' },
    { value: 'mark_until', label: 'Mark read until…' },
] as const

const action = ref<Action | null>(null)
const selectedChildId = ref<string | null>(null)
const queryClient = useQueryClient()
const toast = useToast()

const qChildren = contentApi.useList(
    () => ({
        parent_id: props.contentId,
        sort: 'order',
        sort_order: 'asc',
    }),
    { enabled: computed(() => action.value === 'mark_until') }
)

const childOptions = computed(() =>
    (qChildren.data.value?.data ?? []).map(c => ({ value: c.id, label: c.title }))
)

const mUpdate = useMutation({
    mutationFn: async () => {
        if (action.value === 'reset') {
            await contentApi.setSeriesItemStatuses(props.contentId, null)
        } else if (action.value === 'mark_all') {
            await contentApi.setSeriesItemStatuses(props.contentId, 'completed')
        } else if (action.value === 'mark_until' && selectedChildId.value) {
            await contentApi.setSeriesItemStatuses(
                props.contentId,
                'completed',
                selectedChildId.value
            )
        }
        queryClient.invalidateQueries()
    },
    onSuccess() {
        toast.show({ message: 'Updated the reading progress' })
        props.close()
    },
})
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './UpdateProgressModal.vue'

export function showUpdateProgressModal(contentId: string): Promise<void> {
    return Modals.show(Self, { contentId })
}
</script>
