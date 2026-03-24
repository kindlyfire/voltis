<template>
    <VDialog :model-value="open" @update:model-value="v => !v && close()" max-width="400">
        <VCard>
            <VCardTitle>Set reading status</VCardTitle>
            <VCardText class="space-y-4!">
                <div class="text-medium-emphasis">
                    {{ contentIds.length }} item{{ contentIds.length === 1 ? '' : 's' }} selected
                </div>

                <VSelect
                    v-model="status"
                    :items="readingStatusOptions"
                    label="Status"
                    clearable
                    hide-details
                />

                <VProgressLinear
                    v-if="mBulk.isPending.value"
                    :model-value="(completed / contentIds.length) * 100"
                    color="primary"
                    rounded
                />

                <AQueryError :mutation="mBulk" />
            </VCardText>
            <VCardActions>
                <VSpacer />
                <VBtn variant="text" @click="close()" :disabled="mBulk.isPending.value">
                    Cancel
                </VBtn>
                <VBtn color="primary" :loading="mBulk.isPending.value" @click="mBulk.mutate()">
                    Save
                </VBtn>
            </VCardActions>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { ref } from 'vue'
import AQueryError from '@/components/AQueryError.vue'
import { contentApi } from '@/utils/api/content'
import type { ReadingStatus } from '@/utils/api/types'
import { readingStatusOptions } from '@/utils/misc'

const props = defineProps<{
    open: boolean
    close: () => void
    contentIds: string[]
}>()

const status = ref<ReadingStatus | null>(null)
const completed = ref(0)
const queryClient = useQueryClient()

const mBulk = useMutation({
    mutationFn: async () => {
        completed.value = 0
        for (const id of props.contentIds) {
            await contentApi.updateUserData(id, { status: status.value })
            completed.value++
        }
        await queryClient.invalidateQueries({ queryKey: ['content'] })
        props.close()
    },
})
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './BulkStatusModal.vue'

export function showBulkStatusModal(contentIds: string[]): Promise<void> {
    return Modals.show(Self, { contentIds })
}
</script>
