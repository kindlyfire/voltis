<template>
    <div>
        <div class="mb-2 flex flex-wrap items-center gap-1 gap-y-0">
            <Settings :store-key="storeKey" :width="width" />
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
            <VBtn
                :icon="true"
                variant="text"
                size="small"
                :color="selectMode ? 'primary' : undefined"
                title="Select items"
                @click="toggleSelectMode"
            >
                <VIcon>{{
                    selectMode
                        ? 'mdi-checkbox-multiple-marked-outline'
                        : 'mdi-checkbox-multiple-blank-outline'
                }}</VIcon>
            </VBtn>

            <template v-if="!selectMode">
                <VProgressCircular v-if="loading" indeterminate size="16" width="2" class="ml-2" />
                <span v-else class="pl-2">{{ items.length }} items</span>
            </template>

            <div v-if="selectMode" class="basis-full sm:basis-auto" />
            <template v-if="selectMode">
                <VMenu>
                    <template #activator="{ props: menuProps }">
                        <VBtn variant="text" v-bind="menuProps">Actions</VBtn>
                    </template>
                    <VList density="compact">
                        <VListItem title="Select all" @click="selectAll" />
                    </VList>
                </VMenu>
                <VProgressCircular v-if="loading" indeterminate size="16" width="2" class="ml-2" />
                <span v-else class="pl-2">{{ selectedIds.size }} selected</span>
            </template>
        </div>

        <div v-if="showFilters" class="mb-3 flex flex-row flex-wrap gap-2 sm:items-center">
            <VSelect
                v-model="filters.status.value"
                :items="statusItems"
                label="Status"
                density="compact"
                variant="outlined"
                hide-details
                clearable
                class="w-1/2 sm:w-full sm:max-w-52"
            />

            <VSelect
                v-model="filters.rating.value"
                :items="ratingItems"
                label="Rating"
                density="compact"
                variant="outlined"
                hide-details
                clearable
                class="w-1/2 sm:w-full sm:max-w-52"
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
                class="w-1/2 sm:w-full sm:max-w-52"
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
                <ItemSkeleton />
                <ItemSkeleton />
                <ItemSkeleton />
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
                    @toggle-select="(shiftKey: boolean) => toggleSelect(item.id, shiftKey)"
                />
            </template>
        </div>
    </div>
</template>

<script setup lang="ts">
import { keepPreviousData } from '@tanstack/vue-query'
import { useElementSize } from '@vueuse/core'
import { computed, ref, toRef, watch } from 'vue'
import { contentApi } from '@/utils/api/content'
import {
    READING_STATUS_LABELS,
    type ContentListParams,
    type ReadingStatus,
} from '@/utils/api/types'
import { useRouteQueryParams } from '@/utils/misc'
import Item from './Item.vue'
import ItemSkeleton from './ItemSkeleton.vue'
import Settings from './Settings.vue'
import { useContentGridStore } from './store'

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

const selectMode = ref(false)
const selectedIds = ref(new Set<string>())
const lastSelectedIndex = ref<number | null>(null)

function toggleSelectMode() {
    selectMode.value = !selectMode.value
    selectedIds.value = new Set()
    lastSelectedIndex.value = null
}

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
