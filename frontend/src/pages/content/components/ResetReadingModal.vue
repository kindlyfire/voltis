<template>
    <VDialog :model-value="open" @update:model-value="v => !v && close(false)" max-width="400">
        <VCard>
            <VCardTitle>You've completed this series</VCardTitle>
            <VCardText>
                <div>Do you want to start again? This will mark all chapters as unread.</div>
                <AQueryError :mutation="mResetReading" class="mt-4" />
            </VCardText>

            <VCardActions>
                <VSpacer />
                <VBtn
                    variant="text"
                    @click="close(false)"
                    :disabled="mResetReading.isPending.value"
                >
                    No
                </VBtn>
                <VBtn
                    color="primary"
                    @click="mResetReading.mutate()"
                    :loading="mResetReading.isPending.value"
                >
                    Yes
                </VBtn>
            </VCardActions>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
import { useMutation } from '@tanstack/vue-query'
import AQueryError from '@/components/AQueryError.vue'
import { contentApi } from '@/utils/api/content'

const props = defineProps<{
    open: boolean
    close: (confirmed: boolean) => void
    contentId: string
}>()

const mResetReading = useMutation({
    mutationFn: async () => {
        await contentApi.setSeriesItemStatuses(props.contentId, null)
        await contentApi.updateUserData(props.contentId, { status: 'reading' })
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
