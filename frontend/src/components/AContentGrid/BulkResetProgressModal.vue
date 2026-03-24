<template>
    <VDialog :model-value="open" @update:model-value="v => !v && close()" max-width="500">
        <VCard>
            <VCardTitle>Reset reading progress</VCardTitle>
            <VCardText class="space-y-4!">
                <div class="text-medium-emphasis">
                    This will clear the reading status and progress for the following
                    {{ contentIds.length }} item{{ contentIds.length === 1 ? '' : 's' }}:
                </div>

                <VList density="compact" class="max-h-60 overflow-y-auto">
                    <VListItem
                        v-for="(title, i) in contentTitles"
                        :key="contentIds[i]"
                        :title="title"
                    />
                </VList>

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
                    Confirm
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

const props = defineProps<{
    open: boolean
    close: () => void
    contentIds: string[]
    contentTitles: string[]
    seriesIds: Set<string>
}>()

const completed = ref(0)
const queryClient = useQueryClient()

const mBulk = useMutation({
    mutationFn: async () => {
        completed.value = 0
        for (const id of props.contentIds) {
            if (props.seriesIds.has(id)) {
                await contentApi.setSeriesItemStatuses(id, null)
            }
            await contentApi.updateUserData(id, { status: null, progress: {} })
            completed.value++
        }
        await queryClient.invalidateQueries({ queryKey: ['content'] })
        props.close()
    },
})
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './BulkResetProgressModal.vue'

export function showBulkResetProgressModal(
    contentIds: string[],
    contentTitles: string[],
    seriesIds: Set<string>
): Promise<void> {
    return Modals.show(Self, { contentIds, contentTitles, seriesIds })
}
</script>
