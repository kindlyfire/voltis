<template>
    <VDialog :model-value="open" @update:model-value="v => !v && close()" max-width="500">
        <VCard>
            <VCardTitle>Update progress</VCardTitle>
            <VCardText>
                <VRadioGroup v-model="action" hide-details>
                    <VRadio label="Reset progress" value="reset" />
                    <VRadio label="Mark all as read" value="mark_all" />
                    <VRadio label="Mark read until..." value="mark_until" />
                </VRadioGroup>

                <div v-if="action === 'mark_until'" class="mt-4">
                    <div v-if="qChildren.isLoading.value" class="py-4 text-center">
                        <VProgressCircular indeterminate />
                    </div>
                    <AQueryError :query="qChildren" />

                    <VSelect
                        v-if="qChildren.isSuccess.value"
                        v-model="selectedChildId"
                        :items="qChildren.data.value?.data ?? []"
                        item-title="title"
                        item-value="id"
                        label="Select chapter"
                        messages="Everything up to and including the selected chapter will be marked as completed, and chapters after will be marked unread."
                    />
                </div>

                <AQueryError :mutation="mUpdate" class="mt-4" />
            </VCardText>

            <VCardActions>
                <VSpacer />
                <VBtn variant="text" @click="close()" :disabled="mUpdate.isPending.value">
                    Cancel
                </VBtn>
                <VBtn
                    color="primary"
                    @click="mUpdate.mutate()"
                    :loading="mUpdate.isPending.value"
                    :disabled="!action || (action === 'mark_until' && !selectedChildId)"
                >
                    Confirm
                </VBtn>
            </VCardActions>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { contentApi } from '@/utils/api/content'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import AQueryError from '@/components/AQueryError.vue'

const props = defineProps<{
    open: boolean
    close: () => void
    contentId: string
}>()

type Action = 'reset' | 'mark_all' | 'mark_until'

const action = ref<Action | null>(null)
const selectedChildId = ref<string | null>(null)
const queryClient = useQueryClient()

const qChildren = contentApi.useList(
    () => ({
        parent_id: props.contentId,
        sort: 'order',
        sort_order: 'asc',
    }),
    { enabled: computed(() => action.value === 'mark_until') }
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
