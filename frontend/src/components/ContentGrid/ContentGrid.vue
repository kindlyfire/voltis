<template>
    <DefineToolbar>
        <Settings :store-key="storeKey" :width="width" />
        <AIconButton
            :icon="IconFilter"
            label="Filters"
            :pressed="showFilters"
            :aria-controls="showFilters ? filtersId : undefined"
            @click="toggleFilters"
        />
        <AIconButton
            :icon="IconStar"
            :pressed-icon="IconStarFilled"
            label="Starred only"
            :pressed="starred"
            :class="{ 'text-star': starred }"
            @click="filters.starred.value = starred ? null : 'true'"
        />
        <AIconButton
            :icon="IconSelectMode"
            :pressed-icon="IconSelectModeFilled"
            label="Select items"
            :pressed="selectMode"
            @click="setSelectMode(!selectMode)"
        />
        <AMenu v-if="selectMode">
            <template #trigger>
                <AButton variant="tonal" size="sm" :trailing-icon="IconChevronDown" class="ml-1">
                    Actions
                </AButton>
            </template>
            <AMenuItem
                :leading-icon="IconPlaylistAdd"
                :disabled="selectedIds.size === 0"
                @select="bulk(showBulkListsModal)"
            >
                Add to list
            </AMenuItem>
            <AMenuItem
                :leading-icon="IconBookOpen"
                :disabled="selectedIds.size === 0"
                @select="bulk(showBulkStatusModal)"
            >
                Set reading status
            </AMenuItem>
            <AMenuItem
                v-if="isAdmin"
                :leading-icon="IconMagnifyScan"
                :disabled="selectedIds.size === 0"
                @select="bulk(ids => showScanModal({ contentIds: ids }))"
            >
                Scan
            </AMenuItem>
            <AMenuSeparator />
            <AMenuItem
                :leading-icon="IconRestart"
                tone="danger"
                :disabled="selectedIds.size === 0"
                @select="bulk(ids => showBulkResetProgressModal(ids, selectedTitles()))"
            >
                Reset reading progress
            </AMenuItem>
        </AMenu>
    </DefineToolbar>

    <DefineMeta>
        <span aria-live="polite">
            <ASpinner v-if="qBuckets.isPending.value" size="sm" class="align-middle" />
            <template v-else-if="selectMode">{{ selectedIds.size }} selected</template>
            <template v-else-if="qBuckets.isSuccess.value">{{ plural(total, 'item') }}</template>
        </span>
    </DefineMeta>

    <div class="flex flex-col gap-3.5">
        <APageHeader v-if="title" :title="title">
            <template #actions><ReuseToolbar /></template>
            <template #meta><ReuseMeta /></template>
        </APageHeader>
        <div v-else class="-ml-2 flex flex-wrap items-center gap-1">
            <ReuseToolbar />
            <span class="text-fg-muted ml-2.5 text-sm"><ReuseMeta /></span>
        </div>

        <div
            v-if="showFilters"
            :id="filtersId"
            role="group"
            aria-label="Filters"
            class="flex flex-wrap items-center gap-2"
        >
            <ASelect
                v-model="filters.status.value"
                :options="statusOptions"
                label="Status"
                size="sm"
                clearable
                class="w-[calc(50%-4px)] sm:w-52"
            />
            <ASelect
                v-model="filters.rating.value"
                :options="ratingOptions"
                label="Rating"
                size="sm"
                clearable
                class="w-[calc(50%-4px)] sm:w-52"
            />
            <ASelect
                v-if="!params.sort"
                :model-value="filters.sort.value"
                :options="sortOptions"
                label="Sort"
                size="sm"
                :clearable="
                    filters.sort.value !== FILTER_DEFAULTS.sort ||
                    filters.sort_order.value !== FILTER_DEFAULTS.sort_order
                "
                class="w-[calc(50%-4px)] sm:w-52"
                @update:model-value="onSortChange"
            />
            <AIconButton
                :icon="filters.sort_order.value === 'asc' ? IconSortAscending : IconSortDescending"
                :label="filters.sort_order.value === 'asc' ? 'Ascending' : 'Descending'"
                @click="
                    filters.sort_order.value = filters.sort_order.value === 'asc' ? 'desc' : 'asc'
                "
            />
        </div>

        <QueryError :query="error" />

        <div
            ref="gridRef"
            :aria-busy="pending || undefined"
            @focusin="onFocusin"
            @focusout="onFocusout"
        >
            <div v-if="loading" class="grid gap-x-[18px] gap-y-6" :style="gridStyle">
                <ItemSkeleton v-for="i in cols" :key="i" :hide-title="settings.hideTitle" />
            </div>
            <p
                v-else-if="total === 0 && qBuckets.isSuccess.value"
                class="text-fg-muted py-12 text-center text-sm"
            >
                {{ hasFilters || starred ? 'Nothing matches these filters.' : 'Nothing here yet.' }}
            </p>
            <div v-else class="relative" :style="{ height: `${totalSize}px` }">
                <div
                    v-for="row in rows"
                    :key="row.index"
                    class="absolute inset-x-0 top-0 grid gap-x-[18px]"
                    :style="{
                        ...gridStyle,
                        height: `${rowHeight - ROW_GAP}px`,
                        transform: `translateY(${row.start - scrollMargin}px)`,
                    }"
                >
                    <template v-for="i in rowItems(row.index)" :key="i">
                        <Item
                            v-if="itemAt(i)"
                            :data-index="i"
                            :content="itemAt(i)!"
                            :to-read-route="toReadRoute"
                            :store-key="storeKey"
                            :selecting="selectMode"
                            :selected="selectedIds.has(itemAt(i)!.id)"
                            highlight-reading
                            @toggle-select="
                                (shiftKey: boolean) => toggle(itemAt(i)!.id, i, shiftKey)
                            "
                        />
                        <ItemSkeleton v-else :hide-title="settings.hideTitle" />
                    </template>
                </div>
            </div>
        </div>
    </div>

    <!-- Fixed over the frame's right padding, so it never changes the columns. -->
    <div
        v-if="railShown"
        class="pointer-events-none fixed right-0 bottom-0 z-1 py-2"
        :style="{ top: 'var(--layout-top)', left: `${gridRight}px` }"
    >
        <AScrubber
            :segments="segments"
            :model-value="railValue"
            label="Jump to"
            :tolerance="2 / scrollRange"
            @seek="seek"
            @dragstart="onDragStart"
            @dragend="onDragEnd"
        />
    </div>
</template>

<script setup lang="ts">
import { hashKey, useQueryClient } from '@tanstack/vue-query'
import { defaultRangeExtractor, useWindowVirtualizer, type Range } from '@tanstack/vue-virtual'
import {
    createReusableTemplate,
    useResizeObserver,
    useTimeoutFn,
    useWindowScroll,
    useWindowSize,
} from '@vueuse/core'
import {
    computed,
    nextTick,
    onMounted,
    ref,
    shallowRef,
    toRef,
    useId,
    watch,
    watchEffect,
} from 'vue'
import QueryError from '@/components/QueryError.vue'
import { showScanModal } from '@/pages/settings/ScanModal.vue'
import AButton from '@/ui/AButton.vue'
import AIconButton from '@/ui/AIconButton.vue'
import AMenu from '@/ui/AMenu.vue'
import AMenuItem from '@/ui/AMenuItem.vue'
import AMenuSeparator from '@/ui/AMenuSeparator.vue'
import APageHeader from '@/ui/APageHeader.vue'
import AScrubber from '@/ui/AScrubber.vue'
import ASelect from '@/ui/ASelect.vue'
import ASpinner from '@/ui/ASpinner.vue'
import {
    IconBookOpen,
    IconChevronDown,
    IconFilter,
    IconMagnifyScan,
    IconPlaylistAdd,
    IconRestart,
    IconSelectMode,
    IconSelectModeFilled,
    IconSortAscending,
    IconSortDescending,
    IconStar,
    IconStarFilled,
} from '@/ui/icons'
import { contentWindowKey, PAGE_SIZE } from '@/utils/api/content'
import {
    READING_STATUS_LABELS,
    type Content,
    type ContentListParams,
    type ReadingStatus,
} from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'
import { getLayoutTop, plural, readingStatusOptions, useRouteQueryParams } from '@/utils/misc'
import { useRestoreReady } from '@/utils/whenReachable'
import { showBulkResetProgressModal } from './BulkResetProgressModal.vue'
import { showBulkStatusModal } from './BulkStatusModal.vue'
import Item from './Item.vue'
import ItemSkeleton from './ItemSkeleton.vue'
import { showBulkListsModal } from './ListsModal.vue'
import Settings from './Settings.vue'
import { useContentGridStore } from './store'
import { useContentWindow } from './useContentWindow'
import { useGridSelection } from './useGridSelection'

const props = withDefaults(
    defineProps<{
        params: ContentListParams
        /** Renders the page header (h1) with the toolbar. */
        title?: string
        toReadRoute?: boolean
        storeKey?: string
    }>(),
    { storeKey: 'default' }
)

const store = useContentGridStore()
const settings = store.getForKey(toRef(props, 'storeKey'))

const FILTER_DEFAULTS = {
    status: null as string | null,
    rating: null as string | null,
    sort: 'title',
    sort_order: 'asc',
}
type FilterKey = keyof typeof FILTER_DEFAULTS

const filters = useRouteQueryParams({ starred: null as string | null, ...FILTER_DEFAULTS })

const [DefineToolbar, ReuseToolbar] = createReusableTemplate()
const [DefineMeta, ReuseMeta] = createReusableTemplate()
const filtersId = useId()

const starred = computed(() => filters.starred.value === 'true')

const qMe = usersApi.useMe()
const isAdmin = computed(() => !!qMe.data.value?.permissions.includes('ADMIN'))

const filterKeys = Object.keys(FILTER_DEFAULTS) as FilterKey[]
const hasFilters = computed(() => filterKeys.some(k => filters[k].value !== FILTER_DEFAULTS[k]))

const showFilters = ref(hasFilters.value)

// Hiding the filters also resets them (Starred is its own toggle).
function toggleFilters() {
    showFilters.value = !showFilters.value
    if (!showFilters.value) {
        for (const k of filterKeys) filters[k].value = FILTER_DEFAULTS[k]
    }
}

const statusOptions = [
    { label: 'Has status', value: 'yes' },
    { label: 'No status', value: 'no' },
    ...readingStatusOptions,
]

const ratingOptions = [
    { label: 'Has rating', value: 'yes' },
    { label: 'No rating', value: 'no' },
]

const SORT_DEFAULTS: Record<string, 'asc' | 'desc'> = {
    title: 'asc',
    created_at: 'desc',
    progress_updated_at: 'desc',
    rating: 'desc',
    user_rating: 'desc',
    release_date: 'desc',
    unread_children_count: 'desc',
}

const sortOptions = [
    { label: 'Title', value: 'title' },
    { label: 'Recently added', value: 'created_at' },
    { label: 'Recently read', value: 'progress_updated_at' },
    { label: 'Rating', value: 'rating' },
    { label: 'My rating', value: 'user_rating' },
    { label: 'Release date', value: 'release_date' },
    { label: 'Unread count', value: 'unread_children_count' },
]

function onSortChange(value: string | null) {
    if (value == null) {
        filters.sort.value = FILTER_DEFAULTS.sort
        filters.sort_order.value = FILTER_DEFAULTS.sort_order
    } else {
        filters.sort.value = value
        filters.sort_order.value = SORT_DEFAULTS[value] ?? 'desc'
    }
}

const READING_STATUSES = new Set<string>(Object.keys(READING_STATUS_LABELS))

const queryParams = computed<ContentListParams>(() => {
    const p: ContentListParams = {}

    if (filters.starred.value === 'true') p.starred = true

    const status = filters.status.value
    if (status === 'yes') p.has_status = true
    else if (status === 'no') p.has_status = false
    else if (READING_STATUSES.has(status || '')) p.reading_status = status as ReadingStatus

    const rating = filters.rating.value
    if (rating === 'yes') p.has_rating = true
    else if (rating === 'no') p.has_rating = false

    if (!props.params.sort) {
        p.sort = filters.sort.value as ContentListParams['sort']
    }
    p.sort_order = filters.sort_order.value as ContentListParams['sort_order']

    return { ...props.params, ...p }
})

const paused = ref(false)
const focusedIndex = ref<number | null>(null)
const viewRange = shallowRef({ start: 0, end: 0 })
const { qBuckets, error, total, itemAt, pending } = useContentWindow(
    queryParams,
    viewRange,
    paused,
    focusedIndex
)
// Longer lists render skeletons at full height instead, which keeps the scroll restorable.
const loading = computed(() => pending.value && total.value <= PAGE_SIZE)

const {
    selectMode,
    setSelectMode,
    ids: selectedIds,
    toggle,
    clear: clearSelection,
} = useGridSelection(queryParams)

async function bulk(action: (ids: string[]) => Promise<boolean>) {
    if (await action([...selectedIds.value])) clearSelection()
}

const queryClient = useQueryClient()

// From the cached pages, for a short selection; otherwise the modal shows only the count.
function selectedTitles(): string[] | undefined {
    if (selectedIds.value.size > 50) return
    const titles = new Map<string, string>()
    const key = contentWindowKey(queryParams.value)
    for (const [, page] of queryClient.getQueriesData<{ data?: Content[] }>({ queryKey: key })) {
        for (const c of page?.data ?? []) titles.set(c.id, c.title)
    }
    const out = [...selectedIds.value].map(id => titles.get(id))
    return out.every(t => t !== undefined) ? out : undefined
}

const COL_GAP = 18
const ROW_GAP = 24
// The caption's 10px gap and two 14px × 1.35 title lines.
const CAPTION = 48
// Below Chromium's ~33.5M px layout limit.
const MAX_HEIGHT = 30_000_000
const OVERSCAN = 3

const gridRef = ref<HTMLElement>()
const { height: windowHeight } = useWindowSize()
const { y: scrollY } = useWindowScroll()

function rowHeightFor(cols: number) {
    if (width.value <= 0) return 0
    const colWidth = (width.value - (cols - 1) * COL_GAP) / cols
    return Math.ceil(colWidth * 1.5) + (settings.value.hideTitle ? 0 : CAPTION) + ROW_GAP
}

// More columns than the setting asks for when the rows would exceed MAX_HEIGHT.
const cols = computed(() => {
    if (width.value <= 0) return 1
    let n = Math.max(1, Math.round(width.value / settings.value.itemSize))
    while (Math.ceil(total.value / n) * rowHeightFor(n) > MAX_HEIGHT) n++
    return n
})
const rowHeight = computed(() => rowHeightFor(cols.value))

const gridStyle = computed(() => ({
    gridTemplateColumns: `repeat(${cols.value}, minmax(0, 1fr))`,
}))

// The grid's width, document top and right edge, and the page height.
const width = ref(0)
const scrollMargin = ref(0)
const gridRight = ref(0)
const pageHeight = ref(0)
function measureGrid() {
    const rect = gridRef.value?.getBoundingClientRect()
    if (!rect) return
    width.value = rect.width
    scrollMargin.value = Math.round(rect.top + window.scrollY)
    gridRight.value = rect.right
    pageHeight.value = document.documentElement.scrollHeight
}
// Measured before the first paint, so back navigation can restore the position in that frame.
onMounted(measureGrid)
// The grid moves with anything above it, and sideways with the sidebar.
useResizeObserver([document.body, gridRef], measureGrid)
watch(showFilters, () => nextTick(measureGrid))

const rowCount = computed(() => (width.value > 0 ? Math.ceil(total.value / cols.value) : 0))

const virtualizer = useWindowVirtualizer(
    computed(() => {
        const focusedRow =
            focusedIndex.value === null ? null : Math.floor(focusedIndex.value / cols.value)
        return {
            count: rowCount.value,
            estimateSize: () => rowHeight.value,
            overscan: OVERSCAN,
            scrollMargin: scrollMargin.value,
            scrollPaddingStart: getLayoutTop(),
            // Keeps the focused card rendered, so Tab and Enter still work after scrolling away.
            rangeExtractor: (range: Range) => {
                const out = defaultRangeExtractor(range)
                if (focusedRow === null || focusedRow >= range.count || out.includes(focusedRow)) {
                    return out
                }
                return [...out, focusedRow].sort((a, b) => a - b)
            },
        }
    })
)
// Back/Forward waits for the rows at the target, which the virtualizer starts away from: it
// starts at `scrollY`, a reused grid keeps its offset, and the width narrows once the scrollbar
// appears.
useRestoreReady(
    target => {
        if (width.value <= 0 || qBuckets.isPending.value || loading.value) return false
        const v = virtualizer.value
        let changed = false
        if (gridRef.value!.getBoundingClientRect().width !== width.value) {
            measureGrid()
            changed = true
        }
        // Until reachable, `scrollY` can't be the target, and the restore waits anyway.
        const reachable = target <= document.documentElement.scrollHeight - window.innerHeight
        if (v.scrollOffset !== target && reachable) {
            v.scrollOffset = target
            changed = true
        }
        if (changed) v.measure()
        return !changed
    },
    // The window didn't move, so rows rendered for the target would show blank.
    () => {
        const v = virtualizer.value
        if (v.scrollOffset === window.scrollY) return
        v.scrollOffset = window.scrollY
        v.measure()
    }
)

const rows = computed(() => virtualizer.value.getVirtualItems())
const totalSize = computed(() => virtualizer.value.getTotalSize())

// The viewport rows plus overscan: the rendered rows also hold the focused one, whose page
// `useContentWindow` keeps through `pinned`.
watchEffect(() => {
    void rows.value
    const r = virtualizer.value.range
    viewRange.value = r
        ? {
              start: Math.max(0, r.startIndex - OVERSCAN) * cols.value,
              end: (Math.min(rowCount.value - 1, r.endIndex + OVERSCAN) + 1) * cols.value - 1,
          }
        : { start: 0, end: 0 }
})

function rowItems(row: number) {
    const start = row * cols.value
    const end = Math.min(total.value, start + cols.value)
    return Array.from({ length: end - start }, (_, i) => start + i)
}

function onFocusin(e: FocusEvent) {
    const cell = (e.target as HTMLElement).closest<HTMLElement>('[data-index]')
    focusedIndex.value = cell ? Number(cell.dataset.index) : null
}

function onFocusout(e: FocusEvent) {
    if (!gridRef.value?.contains(e.relatedTarget as Node | null)) focusedIndex.value = null
}

// Keeps the first visible item at the top when the columns or row height change. Not on mount,
// and not while the grid top is visible: the scrollbar appearing narrows the grid on first load.
watch([cols, rowHeight], (_, [oldCols, oldRowHeight]) => {
    virtualizer.value.measure() // rows are never measured, so a new estimate needs this
    const top = window.scrollY + getLayoutTop() - scrollMargin.value
    if (!oldRowHeight || top <= 0) return
    const index = Math.floor(top / oldRowHeight) * oldCols
    void nextTick(() =>
        virtualizer.value.scrollToIndex(Math.floor(index / cols.value), { align: 'start' })
    )
})

// The router keeps the position on query-only changes, which would open a new filter mid-list.
watch(
    () => hashKey([queryParams.value]),
    () => {
        const layoutTop = getLayoutTop()
        if (window.scrollY + layoutTop > scrollMargin.value) {
            window.scrollTo({ top: scrollMargin.value - layoutTop - 16, behavior: 'instant' })
        }
    }
)

// Rail: 0–1 maps the whole page scroll.
const scrollRange = computed(() => Math.max(1, pageHeight.value - windowHeight.value))
const railValue = computed(() => Math.min(1, Math.max(0, scrollY.value / scrollRange.value)))

const NULL_LABELS: Partial<Record<string, [label: string, bubble: string]>> = {
    progress_updated_at: ['–', 'Never'],
    release_date: ['?', 'Unknown'],
}

const segments = computed(() => {
    const nullLabels = NULL_LABELS[queryParams.value.sort ?? ''] ?? ['?', 'Unknown']
    const gridTop = scrollMargin.value - getLayoutTop()
    let index = 0
    return (qBuckets.data.value?.buckets ?? []).map(({ key, count }) => {
        const [label, bubble] = key === null ? nullLabels : [key.toUpperCase(), key.toUpperCase()]
        const rowTop = gridTop + Math.floor(index / cols.value) * rowHeight.value
        // The first segment also covers the header, so it starts at the page top.
        const start = index === 0 ? 0 : Math.min(1, rowTop / scrollRange.value)
        index += count
        return { label, bubble, start }
    })
})

const railShown = computed(
    () => segments.value.length >= 2 && totalSize.value > 3 * windowHeight.value
)

// Pages load once the drag ends or rests for 150ms.
let dragging = false
const { start: resume, stop: stopResume } = useTimeoutFn(() => (paused.value = false), 150, {
    immediate: false,
})

function seek(position: number) {
    window.scrollTo({ top: Math.round(position * scrollRange.value), behavior: 'instant' })
    if (dragging) {
        paused.value = true
        resume()
    }
}

function onDragStart() {
    dragging = true
    paused.value = true
}

function onDragEnd() {
    dragging = false
    stopResume()
    paused.value = false
}
</script>
