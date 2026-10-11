<template>
    <div class="flex flex-col gap-1.5 text-xs">
        <p v-if="preview?.isError" class="text-error flex flex-wrap items-center gap-x-1">
            {{ errorText }}
            <AButton v-if="!badRequest" variant="text" size="sm" @click="emit('retry')">
                Retry
            </AButton>
        </p>
        <p v-else-if="!data" class="text-fg-muted">…</p>
        <template v-else>
            <div class="flex flex-wrap items-center gap-x-2 gap-y-1.5">
                <span v-if="line" class="text-fg-muted">{{ line }}</span>
                <ASegmented
                    v-if="conflicts.length"
                    :model-value="keep"
                    :options="KEEP_OPTIONS"
                    :label="`Reading state to keep for ${group.uri}`"
                    size="sm"
                    @update:model-value="v => emit('update:keep', v)"
                />
            </div>
            <div v-if="series || conflicts.length">
                <AButton
                    variant="text"
                    tone="neutral"
                    size="sm"
                    class="-ml-3"
                    :leading-icon="expanded ? IconChevronDown : IconChevronRight"
                    :aria-expanded="expanded"
                    :aria-controls="regionId"
                    @click="expanded = !expanded"
                >
                    Details
                </AButton>
            </div>
            <div v-if="expanded" :id="regionId" class="flex flex-col gap-3">
                <div v-for="pair in conflicts" :key="pair.from" class="flex flex-col gap-0.5">
                    <code class="[overflow-wrap:anywhere]">{{ pair.from }}</code>
                    <dl class="text-fg-muted grid grid-cols-[4.5rem_1fr] gap-x-2">
                        <dt>This:</dt>
                        <dd>{{ describe(itemAt.get(pair.from) ?? null) }}</dd>
                        <dt>Existing:</dt>
                        <dd>{{ describe(pair.existing) }}</dd>
                    </dl>
                </div>
                <ul v-if="series" ref="list" class="flex flex-col gap-1.5">
                    <li
                        v-for="row in rows"
                        :key="row.src"
                        :data-src="row.src"
                        class="flex items-center gap-1"
                    >
                        <code
                            class="min-w-24 flex-1 text-[13px] [overflow-wrap:anywhere]"
                            :class="{ 'line-through': drop.has(row.src) }"
                            >{{ row.src }}</code
                        >
                        <ACombobox
                            :model-value="row.to"
                            :options="destinations"
                            :label="`Destination for ${row.src}`"
                            hide-label
                            placeholder="Leave broken"
                            size="sm"
                            clearable
                            :disabled="drop.has(row.src)"
                            class="min-w-48 flex-[2]"
                            @update:model-value="v => emit('update:override', row.src, v)"
                        />
                        <AButton
                            v-if="row.src in overrides"
                            variant="text"
                            size="sm"
                            :aria-label="`Reset ${row.src} to automatic pairing`"
                            @click="reset(row.src, $event)"
                        >
                            Reset
                        </AButton>
                        <AIconButton
                            :icon="IconDelete"
                            :label="`Delete the data for ${row.src}`"
                            size="sm"
                            :pressed="drop.has(row.src)"
                            :class="{ invisible: row.to }"
                            :disabled="!!row.to"
                            @click="emit('toggle-drop', row.src)"
                        />
                    </li>
                </ul>
            </div>
        </template>
    </div>
</template>

<script setup lang="ts">
import { computed, ref, useId, useTemplateRef, watch } from 'vue'
import AButton from '@/ui/AButton.vue'
import ACombobox from '@/ui/ACombobox.vue'
import AIconButton from '@/ui/AIconButton.vue'
import ASegmented from '@/ui/ASegmented.vue'
import { IconChevronDown, IconChevronRight, IconDelete } from '@/ui/icons'
import {
    READING_STATUS_LABELS,
    type BrokenRefGroup,
    type BrokenRefsKeep,
    type BrokenRefsPreview,
    type BrokenUserToContent,
} from '@/utils/api/types'
import { RequestError } from '@/utils/fetch'
import { plural } from '@/utils/misc'
import { isSeriesGroup } from './brokenRefs'

/** A group's move onto a target: how its URIs pair, the conflicts, and the leftovers. */
const props = defineProps<{
    group: BrokenRefGroup
    /** Undefined until the preview query exists. */
    preview?: { data?: BrokenRefsPreview; isError: boolean; error: unknown }
    keep: BrokenRefsKeep
    /** Per source URI: its chosen destination, or null to leave it broken. */
    overrides: Record<string, string | null>
    /** Leftover URIs marked for delete. */
    drop: ReadonlyMap<string, unknown>
    contentUris: string[]
}>()
const emit = defineEmits<{
    'update:keep': [keep: BrokenRefsKeep]
    /** A destination, null to leave the source broken, or undefined for the automatic pairing. */
    'update:override': [src: string, dst: string | null | undefined]
    'toggle-drop': [src: string]
    retry: []
}>()

const KEEP_OPTIONS = [
    { value: 'newer', label: 'Keep newer' },
    { value: 'source', label: 'Keep moved' },
    { value: 'target', label: 'Keep existing' },
] as const

const regionId = useId()
const expanded = ref(false)
const series = computed(() => isSeriesGroup(props.group))
const data = computed(() => props.preview?.data)
const badRequest = computed(() => {
    const err = props.preview?.error
    return err instanceof RequestError && err.response?.status === 400
})
// A 400 says why the target can't take the group, and a retry gets the same answer.
const errorText = computed(() =>
    badRequest.value ? RequestError.getMessage(props.preview?.error) : "Couldn't load the preview."
)
const conflicts = computed(() => data.value?.pairs.filter(p => p.existing) ?? [])
const itemAt = computed(() => new Map(props.group.items.map(i => [i.uri, i])))

const line = computed(() => {
    const d = data.value
    if (!d) return ''
    const parts: string[] = []
    // The root always pairs: counting it would add one to every count.
    if (series.value) {
        const matched = d.pairs.filter(p => p.from !== props.group.uri).length
        const left = d.leftovers.filter(u => u !== props.group.uri).length
        parts.push(`${matched} of ${plural(matched + left, 'item')} match`)
        if (left) parts.push(`${left} left over`)
    }
    const merges = d.pairs.reduce((n, p) => n + p.list_merges, 0)
    if (merges)
        parts.push(
            `${plural(merges, 'list entry', 'list entries')} merge${merges === 1 ? 's' : ''}`
        )
    const n = conflicts.value.length
    if (n) parts.push(`${n} already ${n === 1 ? 'has' : 'have'} your data`)
    return parts.join(' · ')
})

const rows = computed(() => {
    const d = data.value
    if (!d) return []
    return [
        ...d.pairs.map(p => ({ src: p.from, to: p.to as string | null })),
        ...d.leftovers.map(src => ({ src, to: null })),
    ].sort((a, b) => a.src.localeCompare(b.src))
})

// One list for every row: a destination another pair uses can't be picked again.
const destinations = computed(() => {
    const d = data.value
    if (!d) return []
    const used = new Set(d.pairs.map(p => p.to))
    const free = new Set(d.free_targets)
    return [...d.free_targets, ...props.contentUris.filter(u => !free.has(u))].map(u => ({
        value: u,
        label: u,
        disabled: used.has(u),
    }))
})

/** Back to the automatic pairing; the button goes, so focus moves to the row's next control. */
function reset(src: string, e: MouseEvent) {
    const row = (e.currentTarget as HTMLElement).closest('li')
    // A leftover marked for delete has a disabled combobox; its delete toggle stays.
    const target = props.drop.has(src) ? 'button[aria-pressed]' : 'input'
    row?.querySelector<HTMLElement>(target)?.focus()
    resetSrc = src
    emit('update:override', src, undefined)
}

// A reset row marked for delete can pair again once the preview refreshes. Its toggle then
// disables and drops focus, so focus moves to its combobox.
const list = useTemplateRef('list')
let resetSrc: string | null = null
watch(
    rows,
    rs => {
        const row = rs.find(r => r.src === resetSrc)
        if (!row?.to) return
        resetSrc = null
        // The browser may not have moved focus off the disabled toggle yet.
        const active = document.activeElement
        if (active !== document.body && !(active instanceof HTMLButtonElement && active.disabled)) {
            return
        }
        list.value
            ?.querySelector<HTMLElement>(`li[data-src="${CSS.escape(row.src)}"] input`)
            ?.focus()
    },
    { flush: 'post' }
)

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
