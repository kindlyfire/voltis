<template>
    <div class="settings-page max-w-[1200px]">
        <APageHeader title="Broken references" />
        <p class="text-fg-muted max-w-[700px] text-sm leading-normal">
            Your reading data and list entries whose content is gone, by series.
        </p>

        <QueryError :query="qSummary" />

        <div v-if="qSummary.isLoading.value" class="flex justify-center py-16">
            <ASpinner size="lg" />
        </div>

        <div
            v-else-if="qSummary.isSuccess.value && !libraryOptions.length"
            class="text-fg-muted flex flex-col items-center gap-2 py-16 text-center"
        >
            <h2
                ref="emptyHeading"
                tabindex="-1"
                class="a-focus text-fg font-display text-xl font-semibold"
            >
                No broken references
            </h2>
            <p class="text-sm">All user data points at existing content.</p>
        </div>

        <template v-else-if="libraryOptions.length">
            <div class="flex flex-wrap items-center gap-3">
                <ASelect
                    v-model="selectedLibraryId"
                    :options="libraryOptions"
                    label="Library"
                    class="w-full sm:w-64"
                />
                <ATextField
                    v-model="searchInput"
                    label="Search refs"
                    type="search"
                    :leading-icon="IconMagnify"
                    clearable
                    class="w-full sm:w-64"
                />
                <div class="flex items-center gap-2 sm:ml-auto">
                    <span v-if="blockedNote" class="text-fg-muted text-sm">{{ blockedNote }}</span>
                    <AMenu align="end">
                        <template #trigger>
                            <AButton variant="tonal" :trailing-icon="IconChevronDown">
                                Bulk actions
                            </AButton>
                        </template>
                        <AMenuItem :leading-icon="IconDelete" @select="markAllDelete">
                            Mark all on this page to delete
                        </AMenuItem>
                        <AMenuItem :leading-icon="IconRestart" @select="unmarkAllDeletes">
                            Unmark all deletes
                        </AMenuItem>
                    </AMenu>
                    <AButton
                        :disabled="!save.ready || !!clash"
                        focusable-when-disabled
                        :loading="mSave.isPending.value"
                        @click="mSave.mutate()"
                    >
                        Save
                    </AButton>
                </div>
            </div>

            <AAlert v-if="clash" tone="danger">Two moves land on {{ clash }}.</AAlert>
            <QueryError :mutation="mSave" closable />
            <QueryError :query="qBrokenRefs" />

            <ACard padding="none">
                <ATable :aria-busy="qBrokenRefs.isFetching.value">
                    <thead>
                        <tr>
                            <th>Broken ref</th>
                            <th>Data</th>
                            <th class="w-1/2">Action</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr v-if="qBrokenRefs.isLoading.value">
                            <td colspan="3"><ASpinner class="mx-auto flex" /></td>
                        </tr>
                        <tr v-else-if="qBrokenRefs.isSuccess.value && !rows.length">
                            <td colspan="3" class="text-fg-muted text-center">
                                {{ search ? 'No refs match the search.' : 'No broken references.' }}
                            </td>
                        </tr>
                        <tr v-for="{ group, move, options } in rows" :key="group.uri">
                            <td class="min-w-48 py-2">
                                <code
                                    class="text-[13px] [overflow-wrap:anywhere]"
                                    :class="{ 'line-through': deletes.has(group.uri) }"
                                    >{{ group.uri }}</code
                                >
                                <div class="text-fg-muted text-xs">{{ counts(group) }}</div>
                            </td>
                            <td class="min-w-36 py-2">
                                <div class="flex items-center gap-1">
                                    <span class="text-fg-muted">{{ summarizeGroup(group) }}</span>
                                    <AIconButton
                                        :icon="IconEye"
                                        :label="`View the broken items of ${group.uri}`"
                                        size="sm"
                                        @click="showBrokenRefDetailModal(group)"
                                    />
                                </div>
                            </td>
                            <td class="py-2">
                                <div class="flex items-start gap-1">
                                    <div class="flex min-w-64 flex-1 flex-col gap-2">
                                        <ACombobox
                                            :model-value="move?.to ?? null"
                                            :options="options"
                                            :label="`Move ${group.uri} to`"
                                            hide-label
                                            placeholder="Move to…"
                                            size="sm"
                                            clearable
                                            :disabled="deletes.has(group.uri)"
                                            :hint="
                                                deletes.has(group.uri)
                                                    ? 'Saving deletes this data.'
                                                    : undefined
                                            "
                                            hint-tone="warning"
                                            @update:model-value="v => setTarget(group.uri, v)"
                                        />
                                        <BrokenRefMove
                                            v-if="move && !deletes.has(group.uri)"
                                            :group="group"
                                            :preview="previews.get(group.uri)"
                                            v-model:keep="move.keep"
                                            :overrides="move.overrides"
                                            :drop="move.drop"
                                            :content-uris="contentUris"
                                            @update:override="
                                                (src, dst) => setOverride(move, src, dst)
                                            "
                                            @toggle-drop="src => toggleDrop(group, move, src)"
                                            @retry="previews.get(group.uri)?.refetch()"
                                        />
                                    </div>
                                    <AIconButton
                                        :icon="IconDelete"
                                        :label="`Delete the data for ${group.uri}`"
                                        size="sm"
                                        :pressed="deletes.has(group.uri)"
                                        class="mt-1"
                                        @click="toggleDelete(group)"
                                    />
                                </div>
                            </td>
                        </tr>
                    </tbody>
                </ATable>
                <footer
                    v-if="pageCount > 1"
                    class="border-outline-variant flex justify-center border-t px-4 py-2"
                >
                    <APagination v-model:page="page" :length="pageCount" label="Broken ref pages" />
                </footer>
            </ACard>
        </template>
    </div>
</template>

<script setup lang="ts">
import { useMutation, useQueries, useQueryClient } from '@tanstack/vue-query'
import { useHead } from '@unhead/vue'
import { refDebounced } from '@vueuse/core'
import { computed, nextTick, reactive, ref, useTemplateRef, watch } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AAlert from '@/ui/AAlert.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import ACombobox from '@/ui/ACombobox.vue'
import AIconButton from '@/ui/AIconButton.vue'
import AMenu from '@/ui/AMenu.vue'
import AMenuItem from '@/ui/AMenuItem.vue'
import APageHeader from '@/ui/APageHeader.vue'
import APagination from '@/ui/APagination.vue'
import ASelect from '@/ui/ASelect.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ATable from '@/ui/ATable.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconChevronDown, IconDelete, IconEye, IconMagnify, IconRestart } from '@/ui/icons'
import type { Option } from '@/ui/options'
import { useToast } from '@/ui/useToast'
import { invalidateMatching } from '@/utils/api/catalog'
import { contentApi } from '@/utils/api/content'
import { customListsKey } from '@/utils/api/custom-lists'
import { librariesApi } from '@/utils/api/libraries'
import { READING_STATUS_LABELS } from '@/utils/api/types'
import type {
    BrokenRefGroup,
    BrokenRefsKeep,
    BrokenRefsPreview,
    BrokenRefsSaveRequest,
    ReadingStatus,
} from '@/utils/api/types'
import { RequestError } from '@/utils/fetch'
import { plural } from '@/utils/misc'
import { showBrokenRefDetailModal } from './BrokenRefDetailModal.vue'
import BrokenRefMove from './BrokenRefMove.vue'
import { isSeriesGroup, summarize } from './brokenRefs'

useHead({ title: 'Broken references' })

const queryClient = useQueryClient()
const toast = useToast()
const emptyHeading = useTemplateRef('emptyHeading')
const qSummary = contentApi.useBrokenRefsSummary()
const qLibraries = librariesApi.useList()

const PAGE_SIZE = 50
const selectedLibraryId = ref<string | null>(null)
const searchInput = ref('')
const search = refDebounced(searchInput, 300)
const page = ref(1)

interface Ids {
    refs: string[]
    entries: string[]
}
interface Move {
    to: string
    keep: BrokenRefsKeep
    overrides: Record<string, string | null>
    /** Leftovers marked for delete, with the ids shown when marked. */
    drop: Map<string, Ids>
}
// Keyed by group URI, and kept across pages. A delete holds the ids shown when it was marked, so
// it deletes exactly those.
const moves = reactive(new Map<string, Move>())
const deletes = reactive(new Map<string, Ids>())

const libraryOptions = computed(() =>
    // Refs whose library was deleted have no library to fix them in.
    (qSummary.data.value ?? []).flatMap(s => {
        if (!s.library_id) return []
        const lib = qLibraries.data.value?.find(l => l.id === s.library_id)
        return [{ value: s.library_id, label: `${lib?.name ?? s.library_id} (${s.count})` }]
    })
)

watch(
    libraryOptions,
    options => {
        if (!options.some(o => o.value === selectedLibraryId.value)) {
            selectedLibraryId.value = options[0]?.value ?? null
        }
    },
    { immediate: true }
)

const qBrokenRefs = contentApi.useBrokenRefs(selectedLibraryId, () => ({
    search: search.value || undefined,
    limit: PAGE_SIZE,
    offset: (page.value - 1) * PAGE_SIZE,
}))
const pageCount = computed(() => Math.ceil((qBrokenRefs.data.value?.total ?? 0) / PAGE_SIZE))
const groups = computed(() => qBrokenRefs.data.value?.data ?? [])

// Saving can shrink the list below the current page, also when the pagination isn't rendered.
watch(
    () => qBrokenRefs.data.value?.total,
    total => {
        if (total == null) return
        page.value = Math.min(page.value, Math.max(1, Math.ceil(total / PAGE_SIZE)))
    }
)

watch([selectedLibraryId, search], () => {
    lastPreview.clear()
    moves.clear()
    deletes.clear()
    page.value = 1
})

const qUris = contentApi.useLibraryUris(selectedLibraryId)
const contentUris = computed(() => qUris.data.value?.content_uris ?? [])

const topLevel = computed(() => {
    const byPrefix = new Map<string, string[]>()
    for (const uri of contentUris.value) {
        const parts = uri.split('/')
        if (parts.length !== 2) continue
        const list = byPrefix.get(parts[0]!) ?? []
        list.push(uri)
        byPrefix.set(parts[0]!, list)
    }
    return byPrefix
})

// A series moves to a top-level URI of its kind, suggestions first; a one-item group anywhere.
function targetOptions(g: BrokenRefGroup): Option<string>[] | string[] {
    if (!isSeriesGroup(g)) return contentUris.value
    const suggested = new Set(g.suggestions.map(s => s.uri))
    const rest = topLevel.value.get(g.uri.split('/')[0]!) ?? []
    return [
        ...g.suggestions.map(s => ({
            value: s.uri,
            label: s.same
                ? `${s.uri} · same series`
                : `${s.uri} · ${s.matched} of ${s.total} match`,
        })),
        ...rest.filter(u => !suggested.has(u)).map(u => ({ value: u, label: u })),
    ]
}
const rows = computed(() =>
    groups.value.map(group => ({
        group,
        move: moves.get(group.uri),
        options: targetOptions(group),
    }))
)

/** A new target pairs anew: the previous choices were about another series. */
function setTarget(group: string, to: string | null) {
    if (!to) moves.delete(group)
    else if (moves.get(group)?.to !== to) {
        moves.set(group, { to, keep: 'newer', overrides: {}, drop: new Map() })
    }
}

function setOverride(move: Move, src: string, dst: string | null | undefined) {
    if (dst === undefined) delete move.overrides[src]
    else move.overrides[src] = dst
}

const idsAt = (g: BrokenRefGroup, uri?: string): Ids => ({
    refs: g.items.filter(i => uri == null || i.uri === uri).map(i => i.id),
    entries: g.list_entries.filter(e => uri == null || e.uri === uri).map(e => e.id),
})

function toggleDrop(g: BrokenRefGroup, move: Move, src: string) {
    if (move.drop.has(src)) move.drop.delete(src)
    else move.drop.set(src, idsAt(g, src))
}

function toggleDelete(g: BrokenRefGroup) {
    if (deletes.has(g.uri)) deletes.delete(g.uri)
    else deletes.set(g.uri, idsAt(g))
}

function markAllDelete() {
    for (const g of groups.value) deletes.set(g.uri, idsAt(g))
}

function unmarkAllDeletes() {
    deletes.clear()
    for (const m of moves.values()) m.drop.clear()
}

// A move's preview is skipped while its group is marked for delete, so no id is both moved and
// deleted.
const moveList = computed(() =>
    [...moves].map(([from, m]) => ({
        from,
        m,
        req: { from, to: m.to, overrides: { ...m.overrides } },
    }))
)
// Each group's last preview. While a changed pair previews, the previous pairs stay on screen, so
// the controls and their focus stay put; Save waits for the fetch. (useQueries makes a new
// observer per key, so keepPreviousData can't do this.)
const lastPreview = new Map<string, { to: string; data: BrokenRefsPreview }>()
const qPreviews = useQueries({
    queries: computed(() =>
        moveList.value.map(({ from, req }) => ({
            ...contentApi.brokenRefsPreviewQuery(selectedLibraryId.value!, req),
            enabled: !deletes.has(from),
            placeholderData: () => {
                const last = lastPreview.get(from)
                return last?.to === req.to ? last.data : undefined
            },
        }))
    ),
})
const previews = computed(
    () => new Map(moveList.value.map(({ from }, i) => [from, qPreviews.value[i]]))
)

// A group with nothing broken left ends its move; a leftover that pairs now can't be deleted.
watch(previews, ps => {
    for (const [from, p] of ps) {
        const m = moves.get(from)
        if (!m || deletes.has(from) || !p?.data || p.isPlaceholderData) continue
        lastPreview.set(from, { to: m.to, data: p.data })
        if (!p.data.pairs.length && !p.data.leftovers.length) moves.delete(from)
        else {
            for (const uri of [...m.drop.keys()]) {
                if (!p.data.leftovers.includes(uri)) m.drop.delete(uri)
            }
        }
    }
})

const save = computed(() => {
    const body: BrokenRefsSaveRequest = { moves: [], delete: { refs: [], list_entries: [] } }
    const add = (r: Ids) => {
        body.delete.refs.push(...r.refs)
        body.delete.list_entries.push(...r.entries)
    }
    // Groups whose preview isn't fresh: Save would send stale or no pairs for them.
    const waiting: string[] = []
    const failed: string[] = []
    for (const r of deletes.values()) add(r)
    for (const { from, m } of moveList.value) {
        if (deletes.has(from)) continue
        const p = previews.value.get(from)
        if (p?.isError) {
            failed.push(from)
            continue
        }
        if (!p?.data || p.isFetching || p.fetchStatus === 'paused' || p.isPlaceholderData) {
            waiting.push(from)
            continue
        }
        const pairs = Object.fromEntries(p.data.pairs.map(pair => [pair.from, pair.to]))
        if (p.data.pairs.length) body.moves.push({ keep: m.keep, pairs })
        // The watch above keeps only drops that are still leftovers.
        for (const r of m.drop.values()) add(r)
    }
    const empty = !body.moves.length && !body.delete.refs.length && !body.delete.list_entries.length
    return { body, waiting, failed, ready: !waiting.length && !failed.length && !empty }
})

// Why Save is disabled, for moves on other pages; this page shows its own.
const blockedNote = computed(() => {
    const here = new Set(groups.value.map(g => g.uri))
    const name = (uris: string[]) => {
        const off = uris.filter(u => !here.has(u))
        return off.length > 1 ? `${off[0]} and ${off.length - 1} more` : off[0]
    }
    const failed = name(save.value.failed)
    if (failed) return `Fix ${failed} first`
    const waiting = name(save.value.waiting)
    return waiting && `Waiting on ${waiting}`
})

const clash = computed(() => {
    const seen = new Set<string>()
    for (const m of save.value.body.moves) {
        for (const dst of Object.values(m.pairs)) {
            if (seen.has(dst)) return dst
            seen.add(dst)
        }
    }
    return undefined
})

const mSave = useMutation({
    mutationFn: () => contentApi.saveBrokenRefs(selectedLibraryId.value!, save.value.body),
    onSuccess: async res => {
        toast.show({
            message: res.moved
                ? `Moved ${plural(res.moved, 'item')}${res.deleted ? `, deleted ${res.deleted}` : ''}`
                : `Deleted ${plural(res.deleted, 'item')}`,
        })
        moves.clear()
        deletes.clear()
        // Moves change user data, list entries, and what reads them.
        await Promise.all([
            invalidateMatching(queryClient, key => key[0] === 'content'),
            queryClient.invalidateQueries({ queryKey: customListsKey }),
        ])
        // Saving the last refs removes the table, and the focused button with it.
        await nextTick()
        emptyHeading.value?.focus()
    },
    onError: async err => {
        if (!(err instanceof RequestError) || err.response?.status !== 409) return
        // The moves stay and preview again; marked ids may be stale, so they're marked anew.
        unmarkAllDeletes()
        await invalidateMatching(queryClient, key => key[0] === 'content')
    },
})

function counts(g: BrokenRefGroup): string {
    const parts: string[] = []
    if (g.items.length) parts.push(plural(g.items.length, 'item'))
    if (g.list_entries.length) {
        parts.push(plural(g.list_entries.length, 'list entry', 'list entries'))
    }
    return parts.join(' · ')
}

function summarizeGroup(g: BrokenRefGroup): string {
    if (!isSeriesGroup(g)) return g.items[0] ? summarize(g.items[0]) : '—'
    const byStatus = new Map<ReadingStatus, number>()
    for (const i of g.items) if (i.status) byStatus.set(i.status, (byStatus.get(i.status) ?? 0) + 1)
    const statuses = Object.keys(READING_STATUS_LABELS) as ReadingStatus[]
    return (
        statuses
            .filter(s => byStatus.has(s))
            .map(s => `${byStatus.get(s)} ${READING_STATUS_LABELS[s].toLowerCase()}`)
            .join(', ') || '—'
    )
}
</script>
