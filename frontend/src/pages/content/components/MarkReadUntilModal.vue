<template>
    <VDialog :model-value="open" @update:model-value="v => !v && close()" max-width="500">
        <VCard>
            <VCardTitle>Mark read until...</VCardTitle>
            <VCardText>
                <div class="mb-4">
                    Everything up to and including the selected chapter will be marked as completed,
                    and chapters after will be marked unread.
                </div>

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
                    hide-details
                />

                <AQueryError :mutation="mMarkUntil" class="mt-4" />
            </VCardText>

            <VCardActions>
                <VSpacer />
                <VBtn variant="text" @click="close()" :disabled="mMarkUntil.isPending.value">
                    Cancel
                </VBtn>
                <VBtn
                    color="primary"
                    @click="mMarkUntil.mutate()"
                    :loading="mMarkUntil.isPending.value"
                    :disabled="!selectedChildId"
                >
                    Confirm
                </VBtn>
            </VCardActions>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { contentApi } from '@/utils/api/content'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import AQueryError from '@/components/AQueryError.vue'

const props = defineProps<{
    open: boolean
    close: () => void
    contentId: string
}>()

const selectedChildId = ref<string | null>(null)
const queryClient = useQueryClient()

const qChildren = contentApi.useList(() => ({
    parent_id: props.contentId,
    sort: 'order',
    sort_order: 'asc',
}))

const mMarkUntil = useMutation({
    mutationFn: async () => {
        if (!selectedChildId.value) return
        await contentApi.setSeriesItemStatuses(props.contentId, 'completed', selectedChildId.value)
        queryClient.invalidateQueries()
        props.close()
    },
})
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './MarkReadUntilModal.vue'

export function showMarkReadUntilModal(contentId: string): Promise<void> {
    return Modals.show(Self, { contentId })
}
</script>
