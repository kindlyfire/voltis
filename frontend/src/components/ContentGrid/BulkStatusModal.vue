<template>
    <ADialog
        :open="open"
        title="Set reading status"
        :description="`${plural(contentIds.length, 'item')} selected`"
        size="sm"
        :dismissible="!mBulk.isPending.value"
        @update:open="v => !v && close()"
    >
        <div class="flex flex-col gap-4">
            <ASelect
                v-model="status"
                :options="readingStatusOptions"
                label="Status"
                placeholder="No status"
                clearable
            />
            <AProgressBar
                v-if="mBulk.isPending.value"
                :value="completed / contentIds.length"
                label="Updating"
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
            <AButton :loading="mBulk.isPending.value" @click="mBulk.mutate()">Save</AButton>
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
import ASelect from '@/ui/ASelect.vue'
import { useToast } from '@/ui/useToast'
import { contentApi } from '@/utils/api/content'
import { READING_STATUS_LABELS, type ReadingStatus } from '@/utils/api/types'
import { plural, readingStatusOptions } from '@/utils/misc'

const props = defineProps<{
    open: boolean
    close: () => void
    contentIds: string[]
}>()

const status = ref<ReadingStatus | null>(null)
const completed = ref(0)
const queryClient = useQueryClient()
const toast = useToast()

const mBulk = useMutation({
    mutationFn: async () => {
        completed.value = 0
        for (const id of props.contentIds) {
            await contentApi.updateUserData(id, { status: status.value })
            completed.value++
        }
        await queryClient.invalidateQueries({ queryKey: ['content'] })
    },
    onSuccess: () => {
        const items = plural(props.contentIds.length, 'item')
        toast.show({
            message: status.value
                ? `Set ${items} to ${READING_STATUS_LABELS[status.value]}`
                : `Cleared the status of ${items}`,
        })
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
