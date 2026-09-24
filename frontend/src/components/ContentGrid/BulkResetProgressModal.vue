<template>
    <ADialog
        :open="open"
        title="Reset reading progress"
        :description="`This clears the reading status and progress of ${plural(contentIds.length, 'item')}:`"
        :dismissible="!mBulk.isPending.value"
        @update:open="v => !v && close()"
    >
        <div class="flex flex-col gap-4">
            <ul
                class="bg-surface-2 rounded-field max-h-60 overflow-y-auto px-4 py-2"
                tabindex="0"
                aria-label="Selected items"
            >
                <li
                    v-for="(title, i) in contentTitles"
                    :key="contentIds[i]"
                    class="border-outline-variant truncate border-t py-2 first:border-t-0"
                >
                    {{ title }}
                </li>
            </ul>
            <AProgressBar
                v-if="mBulk.isPending.value"
                :value="completed / contentIds.length"
                label="Resetting"
                :value-text="`${completed} of ${contentIds.length}`"
            />
            <QueryError :mutation="mBulk" />
        </div>
        <template #actions>
            <AButton
                variant="text"
                tone="neutral"
                :disabled="mBulk.isPending.value"
                @click="close()"
            >
                Cancel
            </AButton>
            <AButton tone="danger" :loading="mBulk.isPending.value" @click="mBulk.mutate()">
                Reset
            </AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { ref } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import AProgressBar from '@/ui/AProgressBar.vue'
import { useToast } from '@/ui/useToast'
import { contentApi } from '@/utils/api/content'
import { plural } from '@/utils/misc'

const props = defineProps<{
    open: boolean
    close: () => void
    contentIds: string[]
    contentTitles: string[]
    seriesIds: Set<string>
}>()

const completed = ref(0)
const queryClient = useQueryClient()
const toast = useToast()

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
    },
    onSuccess: () => {
        toast.show({ message: `Reset the progress of ${plural(props.contentIds.length, 'item')}` })
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
