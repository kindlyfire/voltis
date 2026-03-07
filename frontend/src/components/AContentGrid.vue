<template>
    <div>
        <div class="d-flex align-center gap-1 mb-2">
            <AContentGridSettings :store-key="storeKey" :width="width" />
            <VBtn
                :icon="true"
                variant="text"
                size="small"
                :color="showFilters ? 'primary' : undefined"
                title="Toggle filters"
                @click="toggleFilters"
            >
                <VIcon>{{
                    showFilters ? 'mdi-filter-variant-remove' : 'mdi-filter-variant'
                }}</VIcon>
            </VBtn>
            <VBtn
                :icon="true"
                variant="text"
                size="small"
                :color="filters.starred.value === 'true' ? 'yellow-darken-2' : undefined"
                title="Show starred only"
                @click="filters.starred.value = filters.starred.value === 'true' ? '' : 'true'"
            >
                <VIcon>{{
                    filters.starred.value === 'true' ? 'mdi-star' : 'mdi-star-outline'
                }}</VIcon>
            </VBtn>
            <VProgressCircular v-if="loading" indeterminate size="16" width="2" class="ml-2" />
            <span v-else class="pl-2">{{ items.length }} items</span>
        </div>

        <div v-if="showFilters" class="d-flex align-center flex-wrap gap-2 mb-3">
            <VSelect
                v-model="filters.status.value"
                :items="statusItems"
                label="Status"
                density="compact"
                variant="outlined"
                hide-details
                clearable
                class="max-w-52"
            />

            <VSelect
                v-model="filters.rating.value"
                :items="ratingItems"
                label="Rating"
                density="compact"
                variant="outlined"
                hide-details
                clearable
                class="max-w-52"
            />

            <VSelect
                v-if="!params.sort"
                :model-value="filters.sort.value"
                @update:model-value="onSortChange"
                :items="sortItems"
                label="Sort"
                density="compact"
                variant="outlined"
                hide-details
                :clearable="filters.sort.value !== 'title' || filters.sort_order.value !== 'asc'"
                class="max-w-52"
            />

            <VBtn
                :icon="true"
                variant="text"
                size="small"
                :title="filters.sort_order.value === 'asc' ? 'Ascending' : 'Descending'"
                @click="
                    filters.sort_order.value = filters.sort_order.value === 'asc' ? 'desc' : 'asc'
                "
            >
                <VIcon>{{
                    filters.sort_order.value === 'asc'
                        ? 'mdi-sort-ascending'
                        : 'mdi-sort-descending'
                }}</VIcon>
            </VBtn>
        </div>

        <div ref="gridRef" class="grid gap-4" :style="gridStyle">
            <template v-if="loading">
                <AContentGridItemSkeleton />
                <AContentGridItemSkeleton />
                <AContentGridItemSkeleton />
            </template>

            <template v-else>
                <AContentGridItem
                    v-for="item in items"
                    :key="item.id"
                    :content="item"
                    :to-read-route="toReadRoute"
                    :store-key="storeKey"
                />
            </template>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, ref, toRef } from 'vue'
import { useElementSize } from '@vueuse/core'
import {
    READING_STATUS_LABELS,
    type ContentListParams,
    type ReadingStatus,
} from '@/utils/api/types'
import { contentApi } from '@/utils/api/content'
import { useRouteQueryParams } from '@/utils/misc'
import AContentGridItemSkeleton from './AContentGridItemSkeleton.vue'
import AContentGridItem from './AContentGridItem.vue'
import AContentGridSettings from './AContentGridSettings.vue'
import { useContentGridStore } from './useContentGridStore'
import { keepPreviousData } from '@tanstack/vue-query'

const props = withDefaults(
    defineProps<{
        params: ContentListParams
        toReadRoute?: boolean
        storeKey?: string
    }>(),
    { storeKey: 'default' }
)

const store = useContentGridStore()
const settings = store.getForKey(toRef(props, 'storeKey'))

const filters = useRouteQueryParams({
    starred: null as string | null,
    status: null as string | null,
    rating: null as string | null,
    sort: 'title',
    sort_order: 'asc',
})

const showFilters = ref(
    filters.starred.value != null ||
        filters.status.value != null ||
        filters.rating.value != null ||
        filters.sort.value !== 'title' ||
        filters.sort_order.value !== 'asc'
)

function toggleFilters() {
    showFilters.value = !showFilters.value
    if (!showFilters.value) {
        filters.starred.value = null
        filters.status.value = null
        filters.rating.value = null
        filters.sort.value = 'title'
        filters.sort_order.value = 'asc'
    }
}

const statusItems = [
    { title: 'Has status', value: 'yes' },
    { title: 'No status', value: 'no' },
    ...Object.entries(READING_STATUS_LABELS).map(([value, title]) => ({ title, value })),
]

const ratingItems = [
    { title: 'Has rating', value: 'yes' },
    { title: 'No rating', value: 'no' },
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

const sortItems = [
    { title: 'Title', value: 'title' },
    { title: 'Recently added', value: 'created_at' },
    { title: 'Recently read', value: 'progress_updated_at' },
    { title: 'Rating', value: 'rating' },
    { title: 'My rating', value: 'user_rating' },
    { title: 'Release date', value: 'release_date' },
    { title: 'Unread count', value: 'unread_children_count' },
]

function onSortChange(value: string | null) {
    if (value == null) {
        filters.sort.value = 'title'
        filters.sort_order.value = 'asc'
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
