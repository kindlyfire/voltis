<template>
    <ADialog
        :open="open"
        title="Clear status and position"
        :description="description"
        size="sm"
        :dismissible="!mCommand.isPending.value"
        @update:open="v => !v && close(false)"
    >
        <QueryError :mutation="mCommand" />
        <template #actions>
            <AButton
                variant="text"
                tone="neutral"
                :disabled="mCommand.isPending.value"
                @click="close(false)"
            >
                Cancel
            </AButton>
            <AButton tone="danger" :loading="mCommand.isPending.value" @click="confirm">
                Clear
            </AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import { useToast } from '@/ui/useToast'
import { contentApi } from '@/utils/api/content'
import { readingApi } from '@/utils/api/reading'
import { childNoun } from '@/utils/contentProgress'

const props = defineProps<{
    open: boolean
    close: (cleared: boolean) => void
    contentId: string
    /** Only asks: the caller clears, as the reader does through its sync. */
    confirmOnly?: boolean
}>()

const toast = useToast()
const qContent = contentApi.useGet(() => props.contentId)
const mCommand = readingApi.useCommand()

const description = computed(() => {
    const c = qContent.data.value
    if (c?.type.includes('series')) {
        const n = c.children_count ?? 0
        return `Clears the status, position and reading time of this series and its ${n} ${childNoun(c.type, n)}.`
    }
    return `Clears the status, position and reading time of ${c?.title || 'this item'}.`
})

async function confirm() {
    if (!props.confirmOnly) {
        // A failure shows in the dialog, which stays open.
        const done = await mCommand
            .mutateAsync({ contentId: props.contentId, request: { op: 'clear' } })
            .then(
                () => true,
                () => false
            )
        if (!done) return
        toast.show({ message: 'Cleared the status and position' })
    }
    props.close(true)
}
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './ClearReadingModal.vue'

export function showClearReadingModal(contentId: string, confirmOnly = false): Promise<boolean> {
    return Modals.show<boolean>(Self, { contentId, confirmOnly })
}
</script>
