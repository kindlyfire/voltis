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
            <AMenuItem :leading-icon="IconSelectAll" @select="selectAll">Select all</AMenuItem>
            <AMenuSeparator />
            <AMenuItem
                :leading-icon="IconPlaylistAdd"
                :disabled="selectedIds.size === 0"
                @select="showBulkListsModal([...selectedIds])"
            >
                Add to list
            </AMenuItem>
            <AMenuItem
                :leading-icon="IconBookOpen"
                :disabled="selectedIds.size === 0"
                @select="showBulkStatusModal([...selectedIds])"
            >
                Set reading status
            </AMenuItem>
            <AMenuItem
                :leading-icon="IconMagnifyScan"
                :disabled="selectedIds.size === 0"
                @select="showScanModal({ contentIds: [...selectedIds] })"
            >
                Scan
            </AMenuItem>
            <AMenuSeparator />
            <AMenuItem
                :leading-icon="IconRestart"
                tone="danger"
                :disabled="selectedIds.size === 0"
                @select="
                    showBulkResetProgressModal([...selectedIds], selectedTitles, selectedSeriesIds)
                "
            >
                Reset reading progress
            </AMenuItem>
        </AMenu>
    </DefineToolbar>

    <DefineMeta>
        <span aria-live="polite">
            <ASpinner v-if="loading" size="sm" class="align-middle" />
            <template v-else-if="selectMode">{{ selectedIds.size }} selected</template>
            <template v-else-if="qContents.isSuccess.value">
                {{ plural(items.length, 'item') }}
            </template>
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

        <QueryError :query="qContents" />

        <div
            ref="gridRef"
            class="grid gap-x-[18px] gap-y-6"
            :style="gridStyle"
            :aria-busy="loading || undefined"
        >
            <template v-if="loading">
                <ItemSkeleton v-for="i in Math.max(cols, 3)" :key="i" />
            </template>

            <template v-else>
                <Item
                    v-for="item in items"
                    :key="item.id"
                    :content="item"
                    :to-read-route="toReadRoute"
                    :store-key="storeKey"
                    :selecting="selectMode"
                    :selected="selectedIds.has(item.id)"
                    highlight-reading
                    @toggle-select="(shiftKey: boolean) => toggleSelect(item.id, shiftKey)"
                />
                <p
                    v-if="!items.length && qContents.isSuccess.value"
                    class="text-fg-muted col-span-full py-12 text-center text-sm"
                >
                    {{
                        hasFilters || starred
                            ? 'Nothing matches these filters.'
                            : 'Nothing here yet.'
                    }}
                </p>
            </template>
        </div>
    </div>
</template>

<script setup lang="ts">
import { hashKey, keepPreviousData } from '@tanstack/vue-query'
import { createReusableTemplate, useElementSize } from '@vueuse/core'
import { computed, ref, toRef, useId, watch } from 'vue'
import QueryError from '@/components/QueryError.vue'
import { showScanModal } from '@/pages/settings/ScanModal.vue'
import AButton from '@/ui/AButton.vue'
import AIconButton from '@/ui/AIconButton.vue'
import AMenu from '@/ui/AMenu.vue'
import AMenuItem from '@/ui/AMenuItem.vue'
import AMenuSeparator from '@/ui/AMenuSeparator.vue'
import APageHeader from '@/ui/APageHeader.vue'
import ASelect from '@/ui/ASelect.vue'
import ASpinner from '@/ui/ASpinner.vue'
import {
    IconBookOpen,
    IconChevronDown,
    IconFilter,
    IconMagnifyScan,
    IconPlaylistAdd,
    IconRestart,
    IconSelectAll,
    IconSelectMode,
    IconSelectModeFilled,
    IconSortAscending,
    IconSortDescending,
    IconStar,
    IconStarFilled,
} from '@/ui/icons'
import { contentApi } from '@/utils/api/content'
import {
    READING_STATUS_LABELS,
    type ContentListParams,
    type ReadingStatus,
} from '@/utils/api/types'
import { plural, readingStatusOptions, useRouteQueryParams } from '@/utils/misc'
import { showBulkResetProgressModal } from './BulkResetProgressModal.vue'
import { showBulkStatusModal } from './BulkStatusModal.vue'
import Item from './Item.vue'
import ItemSkeleton from './ItemSkeleton.vue'
import { showBulkListsModal } from './ListsModal.vue'
import Settings from './Settings.vue'
import { useContentGridStore } from './store'

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

const qContents = contentApi.useList(queryParams, {
    placeholderData: keepPreviousData,
})
const items = computed(() => qContents.data.value?.data ?? [])
const loading = qContents.isLoading

const selectMode = ref(false)
const selectedIds = ref(new Set<string>())
const lastSelectedIndex = ref<number | null>(null)

function setSelectMode(on: boolean) {
    selectMode.value = on
    selectedIds.value = new Set()
    lastSelectedIndex.value = null
}

watch(
    () => hashKey([props.params]),
    () => setSelectMode(false)
)

function toggleSelect(id: string, shiftKey: boolean) {
    const next = new Set(selectedIds.value)
    const currentIndex = items.value.findIndex(item => item.id === id)

    if (shiftKey && lastSelectedIndex.value != null && currentIndex !== -1) {
        const lo = Math.min(lastSelectedIndex.value, currentIndex)
        const hi = Math.max(lastSelectedIndex.value, currentIndex)
        for (let i = lo; i <= hi; i++) {
            next.add(items.value[i]!.id)
        }
    } else {
        if (next.has(id)) next.delete(id)
        else next.add(id)
    }

    selectedIds.value = next
    if (currentIndex !== -1) lastSelectedIndex.value = currentIndex
}

function selectAll() {
    selectedIds.value = new Set(items.value.map(item => item.id))
}

const selectedTitles = computed(() => {
    const titleMap = new Map(items.value.map(item => [item.id, item.title]))
    return [...selectedIds.value].map(id => titleMap.get(id) ?? id)
})

const SERIES_TYPES = new Set(['comic_series', 'book_series'])

const selectedSeriesIds = computed(() => {
    const result = new Set<string>()
    for (const item of items.value) {
        if (selectedIds.value.has(item.id) && SERIES_TYPES.has(item.type)) {
            result.add(item.id)
        }
    }
    return result
})

watch(items, items => {
    const newSelectedIds = new Set<string>()
    for (const item of items) {
        if (selectedIds.value.has(item.id)) {
            newSelectedIds.add(item.id)
        }
    }
    selectedIds.value = newSelectedIds
    lastSelectedIndex.value = null
})

const gridRef = ref<HTMLElement>()
const { width } = useElementSize(gridRef)

const cols = computed(() => {
    if (width.value <= 0) return 1
    return Math.max(1, Math.round(width.value / settings.value.itemSize))
})

const gridStyle = computed(() => ({
    gridTemplateColumns: `repeat(${cols.value}, 1fr)`,
}))
</script>
