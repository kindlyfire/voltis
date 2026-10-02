<template>
    <div class="flex flex-col gap-1.5 text-xs">
        <div class="text-fg-muted grid grid-cols-[4.5rem_1fr] gap-x-2">
            <span>This:</span>
            <span>{{ describe(source) }}</span>
            <span>Existing:</span>
            <span v-if="qTarget.isSuccess.value">{{ describe(qTarget.data.value ?? null) }}</span>
            <span v-else-if="qTarget.isError.value" class="text-(--color-error)">
                Couldn't load it.
                <button type="button" class="underline" @click="qTarget.refetch()">Retry</button>
            </span>
            <span v-else>…</span>
        </div>
        <!-- Choosing a side needs both in view: until then, the newer one is kept. -->
        <ASegmented
            :model-value="keep"
            :options="KEEP_OPTIONS"
            :label="`Reading state to keep for ${source.uri}`"
            size="sm"
            :disabled="!qTarget.isSuccess.value"
            @update:model-value="v => emit('update:keep', v)"
        />
    </div>
</template>

<script setup lang="ts">
import { useQuery } from '@tanstack/vue-query'
import { computed } from 'vue'
import ASegmented from '@/ui/ASegmented.vue'
import { contentApi } from '@/utils/api/content'
import { READING_STATUS_LABELS, type BrokenUserToContent } from '@/utils/api/types'

/** A repair onto a URI that has the user's data already: both sides, and which one wins. */
const props = defineProps<{
    source: BrokenUserToContent
    libraryId: string
    uri: string
    keep: Keep
}>()
const emit = defineEmits<{ 'update:keep': [keep: Keep] }>()

const KEEP_OPTIONS = [
    { value: 'source', label: 'Keep this' },
    { value: 'target', label: 'Keep existing' },
    { value: 'newer', label: 'Keep newer' },
] as const

const qTarget = useQuery({
    queryKey: computed(() => previewKey(props.libraryId, props.uri)),
    queryFn: () => contentApi.userDataAt(props.libraryId, props.uri),
})

function describe(u: BrokenUserToContent | null): string {
    if (!u) return '—'
    const p = u.progress
    const position =
        typeof p.current_page === 'number'
            ? `Page ${p.current_page + 1}`
            : typeof p.progress_percent === 'number'
              ? `${Math.round(p.progress_percent)}%`
              : null
    const read = u.last_read_at && `read ${new Date(u.last_read_at).toLocaleDateString()}`
    return (
        [u.status && READING_STATUS_LABELS[u.status], position, read].filter(Boolean).join(' · ') ||
        'No status'
    )
}
</script>

<script lang="ts">
export type Keep = 'source' | 'target' | 'newer'

/** The query of the destination's data, which a choice of side needs to have loaded. */
export const previewKey = (libraryId: string, uri: string) => [
    'content',
    'refs-user-data',
    libraryId,
    uri,
]
</script>
