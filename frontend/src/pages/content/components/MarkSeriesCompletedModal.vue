<template>
    <ADialog
        :open="open"
        title="Mark series completed"
        size="sm"
        :dismissible="!mComplete.isPending.value"
        @update:open="v => !v && close(null)"
    >
        <div class="flex flex-col gap-3">
            <template v-if="series">
                <p class="text-sm">
                    Sets the series' status. Its {{ childNoun(series.type, 2) }} keep theirs unless
                    you include them.
                </p>
                <ACheckbox
                    v-if="series.unread > 0"
                    v-model="includeUnread"
                    :label="`Also mark ${series.unread} unread ${childNoun(series.type, series.unread)} as read`"
                />
            </template>
            <ASpinner v-else-if="qSeries.isLoading.value" class="self-center" />
            <template v-if="qSeries.isError.value">
                <QueryError :query="qSeries" />
                <AButton size="sm" variant="tonal" class="self-start" @click="qSeries.refetch()">
                    Retry
                </AButton>
            </template>
            <QueryError :mutation="mComplete" />
        </div>
        <template #actions>
            <AButton
                variant="text"
                tone="neutral"
                :disabled="mComplete.isPending.value"
                @click="close(null)"
            >
                Cancel
            </AButton>
            <AButton
                :disabled="!series"
                :loading="mComplete.isPending.value"
                @click="known ? close({ includeUnread }) : mComplete.mutate()"
            >
                Mark completed
            </AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { useMutation } from '@tanstack/vue-query'
import { computed, ref } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ACheckbox from '@/ui/ACheckbox.vue'
import ADialog from '@/ui/ADialog.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { useToast } from '@/ui/useToast'
import { contentApi } from '@/utils/api/content'
import { readingApi } from '@/utils/api/reading'
import { childNoun } from '@/utils/contentProgress'

const props = defineProps<{
    open: boolean
    close: (choice: SeriesCompletion | null) => void
    seriesId: string
    /** From the reader, which knows the counts and completes the series itself: this only asks. */
    known?: SeriesCounts
}>()

const toast = useToast()
const qSeries = contentApi.useGet(() => (props.known ? null : props.seriesId))
// Without the counts, the choice to include unread volumes can't be offered: confirming waits.
const series = computed<SeriesCounts | null>(() => {
    const s = qSeries.data.value
    return props.known ?? (s ? { type: s.type, unread: s.unread_children_count ?? 0 } : null)
})
const includeUnread = ref(false)

const mComplete = useMutation({
    mutationFn: () =>
        readingApi.seriesReading(props.seriesId, {
            action: 'mark_series_completed',
            include_unread: includeUnread.value,
        }),
    onSuccess() {
        toast.show({ message: 'Marked the series completed' })
        props.close({ includeUnread: includeUnread.value })
    },
})
</script>

<script lang="ts">
import type { ContentType } from '@/utils/api/types'
import { Modals } from '@/utils/modals'
import Self from './MarkSeriesCompletedModal.vue'

export interface SeriesCompletion {
    includeUnread: boolean
}

export interface SeriesCounts {
    type: ContentType
    unread: number
}

export function showMarkSeriesCompletedModal(
    seriesId: string,
    known?: SeriesCounts
): Promise<SeriesCompletion | null> {
    return Modals.show<SeriesCompletion | null>(Self, { seriesId, known })
}
</script>
