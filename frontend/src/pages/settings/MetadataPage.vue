<template>
    <div class="settings-page flex max-w-[1200px] flex-col gap-4">
        <div class="flex flex-wrap items-end justify-between gap-x-4 gap-y-2">
            <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
                <APageHeader title="Metadata" />
                <template v-if="worker">
                    <AChip size="sm" :tone="activity.tone" aria-live="polite" class="mt-2">
                        {{ activity.label }}
                    </AChip>
                    <APopover label="Metadata worker details" :width="300">
                        <template #trigger>
                            <AButton size="sm" variant="text" tone="neutral" class="mt-2">
                                Details
                            </AButton>
                        </template>
                        <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
                            <template v-for="row in details" :key="row.label">
                                <dt class="text-fg-muted">{{ row.label }}</dt>
                                <dd>{{ row.value }}</dd>
                            </template>
                        </dl>
                    </APopover>
                </template>
            </div>
            <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
                <ASwitch
                    :model-value="paused"
                    label="Pause automatic matching"
                    :disabled="!qSettings.data.value || mSettings.isPending.value"
                    @update:model-value="setPaused"
                />
                <div class="flex flex-wrap gap-2">
                    <AButton
                        variant="tonal"
                        :leading-icon="IconSync"
                        :loading="mRefresh.isPending.value"
                        @click="refreshNow"
                    >
                        Refresh metadata
                    </AButton>
                    <ATooltip text="Automatic matching is off" :disabled="!matchOff">
                        <AButton
                            :leading-icon="IconLink"
                            :loading="mMatch.isPending.value"
                            :disabled="matchOff"
                            focusable-when-disabled
                            @click="matchNow"
                        >
                            Match now
                        </AButton>
                    </ATooltip>
                </div>
            </div>
        </div>
        <p v-if="warnings.length" class="text-danger text-sm">{{ warnings.join(' · ') }}</p>

        <QueryError :mutation="mSettings" closable />
        <QueryError :mutation="mMatch" closable />
        <QueryError :mutation="mRefresh" closable />

        <div class="flex flex-wrap items-center gap-3">
            <ASegmented v-model="tab" :options="tabOptions" label="Series" />
            <ATextField
                v-model="searchInput"
                label="Search titles"
                type="search"
                :leading-icon="IconMagnify"
                clearable
                size="sm"
                class="w-full sm:w-56"
            />
            <ACheckbox
                v-if="tab === 'unmatched' || tab === 'auto'"
                v-model="failed"
                label="Errors only"
                size="sm"
            />
            <ASelect
                v-model="libraryId"
                :options="libraryOptions"
                label="Library"
                placeholder="All libraries"
                clearable
                size="sm"
                class="w-full sm:ml-auto sm:w-56"
            />
        </div>

        <div v-if="tab !== 'ignored'" class="flex flex-wrap items-center gap-2">
            <span class="text-fg-muted text-sm">{{ selected.size }} selected</span>
            <AButton
                v-if="tab === 'review'"
                size="sm"
                variant="tonal"
                :disabled="!selected.size || mResolve.isPending.value"
                @click="resolve(selectedItems.flatMap(accept))"
            >
                Accept selected
            </AButton>
            <AButton
                v-if="tab === 'review'"
                size="sm"
                variant="text"
                tone="danger"
                :disabled="!selected.size || mResolve.isPending.value"
                @click="resolve(selectedItems.map(reject))"
            >
                Reject selected
            </AButton>
            <AButton
                size="sm"
                variant="text"
                tone="danger"
                :disabled="!selected.size || mResolve.isPending.value"
                @click="resolve(selectedItems.map(ignore))"
            >
                Ignore selected
            </AButton>
        </div>

        <QueryError :query="qReview" />
        <QueryError :mutation="mResolve" closable />
        <QueryError :mutation="mAction" closable />

        <ACard padding="none">
            <ATable :aria-busy="qReview.isFetching.value">
                <thead>
                    <tr>
                        <th v-if="tab !== 'ignored'" class="w-10">
                            <ACheckbox
                                :model-value="allSelected"
                                :indeterminate="selected.size > 0 && !allSelected"
                                label="Select all on this page"
                                hide-label
                                :disabled="!items.length"
                                @update:model-value="selectAll"
                            />
                        </th>
                        <th>Series</th>
                        <th>{{ ENTRY_HEADING[tab] }}</th>
                        <th><span class="sr-only">Actions</span></th>
                    </tr>
                </thead>
                <tbody>
                    <tr v-if="qReview.isLoading.value">
                        <td colspan="4"><ASpinner class="mx-auto flex" /></td>
                    </tr>
                    <tr v-else-if="qReview.isSuccess.value && !items.length">
                        <td colspan="4" class="text-fg-muted text-center">
                            {{ search || failedFilter ? 'No matches' : EMPTY[tab] }}
                        </td>
                    </tr>
                    <tr v-for="item in items" :key="keyOf(item)">
                        <td v-if="tab !== 'ignored'" class="py-2">
                            <ACheckbox
                                :model-value="selected.has(keyOf(item))"
                                :label="`Select ${titleOf(item)}`"
                                hide-label
                                @update:model-value="v => toggle(item, v)"
                            />
                        </td>
                        <td class="min-w-48 py-2">
                            <RouterLink
                                :to="`/${item.content.id}`"
                                class="a-focus rounded-sm font-medium hover:underline"
                            >
                                {{ titleOf(item) }}
                            </RouterLink>
                            <div class="text-fg-muted text-xs">
                                {{ libraryName(item.content.library_id) }} ·
                                {{ item.link.label }}
                            </div>
                        </td>
                        <td class="min-w-48 py-2">
                            <template v-if="entryOf(item)">
                                <a
                                    :href="entryOf(item)!.url"
                                    target="_blank"
                                    rel="noopener"
                                    class="a-focus text-primary rounded-sm font-medium hover:underline"
                                >
                                    {{ entryOf(item)!.title }}
                                    <span class="sr-only">(opens in a new tab)</span>
                                </a>
                                <span v-if="entryOf(item)!.year" class="text-fg-muted">
                                    ({{ entryOf(item)!.year }})
                                </span>
                                <div
                                    v-if="tab === 'review'"
                                    class="mt-1 flex flex-wrap items-center gap-1"
                                >
                                    <AChip
                                        v-for="chip in evaluationChips(
                                            item.link.candidates[0].evaluation
                                        )"
                                        :key="chip.label"
                                        size="sm"
                                        variant="outlined"
                                        :tone="chip.tone"
                                    >
                                        {{ chip.label }}
                                    </AChip>
                                    <span
                                        v-if="item.link.candidates.length > 1"
                                        class="text-fg-muted text-xs"
                                    >
                                        {{
                                            plural(
                                                item.link.candidates.length - 1,
                                                'other candidate',
                                                'other candidates'
                                            )
                                        }}
                                    </span>
                                </div>
                            </template>
                            <p v-if="item.link.last_error" class="text-danger text-sm">
                                {{ tab === 'auto' ? "Can't read its data" : 'Failed' }}:
                                {{ item.link.last_error }}
                            </p>
                            <p v-else-if="!entryOf(item)" class="text-fg-muted text-sm">
                                {{ STATUS[tab] }}
                            </p>
                            <p v-if="errors.has(keyOf(item))" class="text-danger text-sm">
                                {{ errors.get(keyOf(item)) }}
                            </p>
                        </td>
                        <td class="w-px py-2 whitespace-nowrap">
                            <div class="flex justify-end gap-1">
                                <AButton
                                    v-if="tab === 'review'"
                                    size="sm"
                                    variant="tonal"
                                    :disabled="mResolve.isPending.value"
                                    @click="resolve(accept(item))"
                                >
                                    Accept
                                </AButton>
                                <AButton
                                    v-if="tab === 'ignored'"
                                    size="sm"
                                    variant="tonal"
                                    :loading="isRematching(item)"
                                    :disabled="mAction.isPending.value"
                                    @click="rematch(item)"
                                >
                                    Rematch
                                </AButton>
                                <AButton size="sm" variant="text" @click="choose(item)">
                                    Choose…
                                </AButton>
                                <AButton
                                    v-if="tab === 'review'"
                                    size="sm"
                                    variant="text"
                                    tone="danger"
                                    :disabled="mResolve.isPending.value"
                                    @click="resolve([reject(item)])"
                                >
                                    Reject
                                </AButton>
                                <AButton
                                    v-if="tab !== 'ignored'"
                                    size="sm"
                                    variant="text"
                                    tone="danger"
                                    :disabled="mResolve.isPending.value"
                                    @click="resolve([ignore(item)])"
                                >
                                    Ignore
                                </AButton>
                            </div>
                        </td>
                    </tr>
                </tbody>
            </ATable>
            <footer
                v-if="pageCount > 1"
                class="border-outline-variant flex justify-center border-t px-4 py-2"
            >
                <APagination v-model:page="page" :length="pageCount" label="Series pages" />
            </footer>
        </ACard>
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { formatTimeAgo, refDebounced, useNow } from '@vueuse/core'
import { computed, reactive, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import QueryError from '@/components/QueryError.vue'
import { evaluationChips } from '@/pages/content/components/evaluationChips'
import { showProviderSearchModal } from '@/pages/content/components/ProviderSearchModal.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import ACheckbox from '@/ui/ACheckbox.vue'
import AChip from '@/ui/AChip.vue'
import APageHeader from '@/ui/APageHeader.vue'
import APagination from '@/ui/APagination.vue'
import APopover from '@/ui/APopover.vue'
import ASegmented from '@/ui/ASegmented.vue'
import ASelect from '@/ui/ASelect.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ASwitch from '@/ui/ASwitch.vue'
import ATable from '@/ui/ATable.vue'
import ATextField from '@/ui/ATextField.vue'
import ATooltip from '@/ui/ATooltip.vue'
import { IconLink, IconMagnify, IconSync } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { librariesApi } from '@/utils/api/libraries'
import {
    metadataApi,
    type MatchCounts,
    type RefreshCounts,
    type ReviewAction,
    type ReviewItem,
    type ReviewTab,
    type WorkerPass,
} from '@/utils/api/metadata'
import { settingsApi, settingValue } from '@/utils/api/settings'
import { libraryAutoMatches } from '@/utils/librarySettings'
import { plural } from '@/utils/misc'

useHead({ title: 'Metadata' })

const PAGE_SIZE = 50

const ENTRY_HEADING: Record<ReviewTab, string> = {
    review: 'Best candidate',
    unmatched: 'Status',
    auto: 'Linked to',
    ignored: 'Status',
}
const STATUS: Record<ReviewTab, string> = {
    review: '',
    unmatched: 'No match found',
    auto: '',
    ignored: 'Ignored',
}
const EMPTY: Record<ReviewTab, string> = {
    review: 'Nothing to review',
    unmatched: 'None',
    auto: 'None',
    ignored: 'None',
}

const toast = useToast()
const tab = ref<ReviewTab>('review')
const libraryId = ref<string | null>(null)
const searchInput = ref('')
const search = refDebounced(
    computed(() => searchInput.value.trim()),
    300
)
const failed = ref(false)
// Only the tabs whose rows can fail offer the filter.
const failedFilter = computed(
    () => failed.value && (tab.value === 'unmatched' || tab.value === 'auto')
)
const page = ref(1)

const qLibraries = librariesApi.useList()
const qSummary = metadataApi.useSummary()
const libraryOptions = computed(() =>
    (qLibraries.data.value ?? []).map(l => ({ value: l.id, label: l.name }))
)
const counts = computed(() => {
    const rows = (qSummary.data.value?.libraries ?? []).filter(
        s => !libraryId.value || s.library_id === libraryId.value
    )
    return {
        review: rows.reduce((n, s) => n + s.review, 0),
        unmatched: rows.reduce((n, s) => n + s.unmatched, 0),
    }
})
const tabOptions = computed(() => [
    { value: 'review' as const, label: `Needs review (${counts.value.review})` },
    {
        value: 'unmatched' as const,
        label: `No match (${counts.value.unmatched})`,
    },
    { value: 'auto' as const, label: 'Auto-linked' },
    { value: 'ignored' as const, label: 'Ignored' },
])

function libraryName(id: string) {
    return qLibraries.data.value?.find(l => l.id === id)?.name ?? id
}

const worker = computed(() => qSummary.data.value?.worker)
const activity = computed(() => {
    const w = worker.value
    if (w?.activity === 'recomputing')
        return { label: `Updating metadata… ${w.stale} left`, tone: 'primary' } as const
    if (w?.activity === 'matching') {
        return { label: `Matching ${libraryName(w.library_id ?? '')}…`, tone: 'primary' } as const
    }
    if (w?.activity === 'refreshing') return { label: 'Refreshing…', tone: 'primary' } as const
    return w?.paused
        ? ({ label: 'Paused', tone: 'warning' } as const)
        : ({ label: 'Idle', tone: 'neutral' } as const)
})

const qConfig = metadataApi.useConfig()
const providerLabel = (name: string) =>
    qConfig.data.value?.providers.find(p => p.name === name)?.label ?? name
const warnings = computed(() =>
    (qSummary.data.value?.providers ?? [])
        .filter(h => h.failing || h.undecodable)
        .map(h => {
            const parts = [
                h.failing && `${plural(h.failing, 'refresh', 'refreshes')} failing`,
                h.undecodable && `${h.undecodable} unreadable`,
            ]
            return `${providerLabel(h.provider)}: ${parts.filter(Boolean).join(', ')}`
        })
)

const now = useNow({ interval: 30_000 })
const ago = (at: string) => formatTimeAgo(new Date(at), {}, now.value)
const matchCounts = (c: MatchCounts) =>
    [
        `${c.linked} linked`,
        `${c.review} review`,
        `${c.unmatched} unmatched`,
        c.failed && `${c.failed} failed`,
        c.skipped && `${c.skipped} skipped`,
    ]
        .filter(Boolean)
        .join(', ')
const refreshCounts = (c: RefreshCounts) =>
    [`${c.refreshed} refreshed`, c.failed && `${c.failed} failed`].filter(Boolean).join(', ')
function passValue<T extends object>(pass: WorkerPass<T>, counts: (c: T) => string) {
    if (!Object.values(pass.counts).some(Boolean)) return '—'
    return `${pass.finished ? ago(pass.finished) : 'Running'}: ${counts(pass.counts)}`
}
const details = computed(() => {
    const w = worker.value!
    return [
        { label: 'Last match', value: passValue(w.match_pass, matchCounts) },
        { label: 'Last refresh', value: passValue(w.refresh_pass, refreshCounts) },
        { label: 'Since start', value: `${matchCounts(w.matched)}, ${refreshCounts(w.refreshed)}` },
        ...(qSummary.data.value?.providers ?? []).map(h => ({
            label: `${providerLabel(h.provider)} fetched`,
            value: h.last_fetched ? ago(h.last_fetched) : 'Never',
        })),
    ]
})

const PAUSED = 'metadata.matching_paused'
const qSettings = settingsApi.useList()
const mSettings = settingsApi.useUpdate()
const paused = computed(() => settingValue(qSettings.data.value, PAUSED, false))

function setPaused(on: boolean) {
    mSettings.mutate({ [PAUSED]: on })
}

// Match now only matches libraries that match automatically. Not off while that is unknown.
const matchOff = computed(() => {
    const libs = qLibraries.data.value?.filter(l => !libraryId.value || l.id === libraryId.value)
    const providers = qConfig.data.value?.providers.map(p => p.name)
    return !!libs && !!providers && !libs.some(l => libraryAutoMatches(l, providers))
})

const qReview = metadataApi.useReview(() => ({
    tab: tab.value,
    libraryId: libraryId.value,
    search: search.value,
    failed: failedFilter.value,
    limit: PAGE_SIZE,
    offset: (page.value - 1) * PAGE_SIZE,
}))
const items = computed(() => qReview.data.value?.data ?? [])
const pageCount = computed(() => Math.ceil((qReview.data.value?.total ?? 0) / PAGE_SIZE))

const keyOf = (item: ReviewItem) => `${item.content.id}:${item.link.provider}`
const titleOf = (item: ReviewItem) => item.local_title || item.content.title

function entryOf(item: ReviewItem) {
    if (tab.value === 'review') return item.link.candidates[0]
    if (tab.value === 'auto') return item.link.entry
    return null
}

// Selection and failed results are per row; both reset with the list they belong to.
const selected = reactive(new Set<string>())
const errors = reactive(new Map<string, string>())
watch([tab, libraryId, search, failedFilter], () => {
    page.value = 1
    selected.clear()
    errors.clear()
})
watch(page, () => selected.clear())
// Resolving the last rows of the last page leaves it past the end.
watch(pageCount, count => {
    if (qReview.data.value) page.value = Math.min(page.value, Math.max(1, count))
})

const selectedItems = computed(() => items.value.filter(i => selected.has(keyOf(i))))
const allSelected = computed(
    () => items.value.length > 0 && items.value.every(i => selected.has(keyOf(i)))
)

function toggle(item: ReviewItem, on: boolean) {
    if (on) selected.add(keyOf(item))
    else selected.delete(keyOf(item))
}

function selectAll(on: boolean) {
    for (const item of items.value) toggle(item, on)
}

function accept(item: ReviewItem): ReviewAction[] {
    const top = item.link.candidates[0]
    if (!top) return []
    return [
        {
            content_id: item.content.id,
            provider: item.link.provider,
            action: 'link',
            external_id: top.key.id,
            expect_rev: item.link.rev,
        },
    ]
}

function reject(item: ReviewItem): ReviewAction {
    return {
        content_id: item.content.id,
        provider: item.link.provider,
        action: 'reject',
        external_ids: item.link.candidates.map(c => c.key.id),
        expect_rev: item.link.rev,
    }
}

function ignore(item: ReviewItem): ReviewAction {
    return {
        content_id: item.content.id,
        provider: item.link.provider,
        action: 'ignore',
        expect_rev: item.link.rev,
    }
}

const mResolve = metadataApi.useResolveReview()

/** Resolves items one by one; the ones that failed stay listed, with their error. */
function resolve(actions: ReviewAction[]) {
    if (!actions.length) return
    mResolve.mutate(actions, {
        onSuccess({ results }) {
            let done = 0
            for (const r of results) {
                const key = `${r.content_id}:${r.provider}`
                selected.delete(key)
                if (r.ok) {
                    done++
                    errors.delete(key)
                } else {
                    errors.set(key, r.error ?? 'Failed')
                }
            }
            const failed = results.length - done
            toast.show({
                message: failed
                    ? `${done} done, ${failed} failed`
                    : `${plural(done, 'series', 'series')} done`,
                tone: failed ? 'danger' : undefined,
            })
        },
    })
}

const mAction = metadataApi.useLinkAction()

function isRematching(item: ReviewItem) {
    const v = mAction.variables.value
    return mAction.isPending.value && v?.action === 'rematch' && v.contentId === item.content.id
}

function rematch(item: ReviewItem) {
    mAction.mutate(
        {
            action: 'rematch',
            contentId: item.content.id,
            provider: item.link.provider,
            rev: item.link.rev,
        },
        {
            onSuccess: () => toast.show({ message: `Matched ${titleOf(item)} again` }),
        }
    )
}

function choose(item: ReviewItem) {
    showProviderSearchModal(item.content.id, item.link, titleOf(item))
}

const mMatch = metadataApi.useMatchNow()
const mRefresh = metadataApi.useRefreshNow()

function matchNow() {
    mMatch.mutate(libraryId.value ? [libraryId.value] : undefined, {
        onSuccess: () => toast.show({ message: 'Matching' }),
    })
}

function refreshNow() {
    mRefresh.mutate(undefined, {
        onSuccess: () => toast.show({ message: 'Refreshing' }),
    })
}
</script>
