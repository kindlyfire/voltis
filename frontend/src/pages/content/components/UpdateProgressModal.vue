<template>
    <ADialog
        :open="open"
        title="Update progress"
        :dismissible="!mUpdate.isPending.value"
        @update:open="v => !v && close()"
    >
        <div class="flex flex-col gap-4">
            <ARadioGroup v-model="action" :options="actionOptions" label="Action" hide-label />

            <template v-if="action === 'mark_through'">
                <div v-if="qChildren.isLoading.value" class="flex justify-center py-4">
                    <ASpinner />
                </div>
                <QueryError :query="qChildren" />
                <ACombobox
                    v-if="qChildren.isSuccess.value"
                    v-model="selectedChildId"
                    :options="childOptions"
                    :label="capitalize(childNoun(type, 1))"
                    :placeholder="`Select a ${childNoun(type, 1)}`"
                    :hint="`${capitalize(childNoun(type, 2))} up to and including the selected one are marked completed. Others are unchanged.`"
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
                :disabled="!action || (action === 'mark_through' && !selectedChildId)"
                @click="confirm"
            >
                Confirm
            </AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { useMutation } from '@tanstack/vue-query'
import { capitalize, computed, ref } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ACombobox from '@/ui/ACombobox.vue'
import ADialog from '@/ui/ADialog.vue'
import ARadioGroup from '@/ui/ARadioGroup.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { useToast } from '@/ui/useToast'
import { contentApi } from '@/utils/api/content'
import { readingApi } from '@/utils/api/reading'
import { childNoun } from '@/utils/contentProgress'
import { showClearReadingModal } from './ClearReadingModal.vue'

const props = defineProps<{
    open: boolean
    close: () => void
    contentId: string
}>()

type Action = 'clear' | 'mark_all' | 'mark_through'

const actionOptions = [
    { value: 'clear', label: 'Clear status and position' },
    { value: 'mark_all', label: 'Mark all as read' },
    { value: 'mark_through', label: 'Mark read through…' },
] as const

const action = ref<Action | null>(null)
const selectedChildId = ref<string | null>(null)
const toast = useToast()
const qSeries = contentApi.useGet(() => props.contentId)
const type = computed(() => qSeries.data.value?.type ?? 'comic_series')

const qChildren = contentApi.useList(
    () => ({
        parent_id: props.contentId,
        sort: 'order',
        sort_order: 'asc',
    }),
    { enabled: computed(() => action.value === 'mark_through') }
)

const childOptions = computed(() =>
    (qChildren.data.value?.data ?? []).map(c => ({ value: c.id, label: c.title }))
)

const mUpdate = useMutation({
    mutationFn: async () => {
        // "Mark all" is marking through the last volume.
        const until =
            action.value === 'mark_all'
                ? (
                      await contentApi.ids(
                          { parent_id: props.contentId, sort: 'order', sort_order: 'desc' },
                          0,
                          1
                      )
                  ).ids[0]
                : selectedChildId.value
        if (!until) throw new Error(`This series has no ${childNoun(type.value, 2)}.`)
        await readingApi.seriesReading(props.contentId, { action: 'mark_through', until_id: until })
    },
    onSuccess() {
        toast.show({ message: 'Updated the reading progress' })
        props.close()
    },
})

// The shared confirmation, which names what it clears, takes over.
async function confirm() {
    if (action.value !== 'clear') return mUpdate.mutate()
    props.close()
    await showClearReadingModal(props.contentId)
}
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './UpdateProgressModal.vue'

export function showUpdateProgressModal(contentId: string): Promise<void> {
    return Modals.show(Self, { contentId })
}
</script>
