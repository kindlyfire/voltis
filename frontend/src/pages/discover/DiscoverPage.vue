<template>
    <div class="page-frame flex max-w-[960px] flex-col gap-4">
        <APageHeader title="Discover" />

        <div class="flex flex-wrap items-center gap-3">
            <ASegmented v-model="kind" :options="KIND_OPTIONS" label="Kind" />
            <ATextField
                v-model="searchInput"
                label="Filter"
                type="search"
                :leading-icon="IconMagnify"
                clearable
                size="sm"
                class="w-full sm:w-56"
            />
            <ASelect
                v-model="library"
                :options="libraryOptions"
                label="Library"
                placeholder="All libraries"
                clearable
                size="sm"
                class="w-full sm:ml-auto sm:w-56"
            />
        </div>

        <QueryError :query="qList" />

        <ACard padding="none">
            <ATable :aria-busy="qList.isFetching.value">
                <thead>
                    <tr>
                        <ASortHeader
                            :sort="sort === 'name' ? order : null"
                            @toggle="toggleSort('name')"
                        >
                            Name
                        </ASortHeader>
                        <ASortHeader
                            :sort="sort === 'count' ? order : null"
                            class="w-px"
                            @toggle="toggleSort('count')"
                        >
                            Count
                        </ASortHeader>
                    </tr>
                </thead>
                <tbody>
                    <tr v-if="qList.isLoading.value">
                        <td colspan="2"><ASpinner class="mx-auto flex" /></td>
                    </tr>
                    <tr v-else-if="qList.isSuccess.value && !rows.length">
                        <td colspan="2" class="text-fg-muted text-center">
                            {{ query.q.value ? 'No matches' : 'None yet' }}
                        </td>
                    </tr>
                    <tr v-for="row in rows" :key="row.key">
                        <td class="py-2">
                            <RouterLink
                                :to="facetRoute(kind, row.key, library ? { library } : undefined)"
                                class="a-focus rounded-sm font-medium [overflow-wrap:anywhere] hover:underline"
                            >
                                {{ kind === 'genres' ? slugLabel(row.name) : row.name }}
                            </RouterLink>
                        </td>
                        <td class="text-fg-muted text-end">{{ row.count }}</td>
                    </tr>
                </tbody>
            </ATable>
            <footer
                v-if="pageCount > 1"
                class="border-outline-variant flex justify-center border-t px-4 py-2"
            >
                <APagination v-model:page="page" :length="pageCount" label="Pages" />
            </footer>
        </ACard>
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { refDebounced } from '@vueuse/core'
import { computed, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import QueryError from '@/components/QueryError.vue'
import ACard from '@/ui/ACard.vue'
import APageHeader from '@/ui/APageHeader.vue'
import APagination from '@/ui/APagination.vue'
import ASegmented from '@/ui/ASegmented.vue'
import ASelect from '@/ui/ASelect.vue'
import ASortHeader from '@/ui/ASortHeader.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ATable from '@/ui/ATable.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconMagnify } from '@/ui/icons'
import { facetsApi } from '@/utils/api/facets'
import { librariesApi } from '@/utils/api/libraries'
import type { FacetKind } from '@/utils/api/types'
import { facetRoute, slugLabel } from '@/utils/facets'
import { useRouteQueryParams } from '@/utils/misc'

useHead({ title: 'Discover' })

const PAGE_SIZE = 100
const KIND_OPTIONS: { value: FacetKind; label: string }[] = [
    { value: 'genres', label: 'Genres' },
    { value: 'tags', label: 'Tags' },
    { value: 'people', label: 'People' },
    { value: 'publishers', label: 'Publishers' },
]
type Sort = 'count' | 'name'
const DEFAULT_ORDER = { count: 'desc', name: 'asc' } as const

const query = useRouteQueryParams({
    tab: 'genres',
    q: '',
    sort: 'count',
    order: null as string | null,
    page: '1',
    library: null as string | null,
})

// Changing what the list shows starts it over at the first page; the filter carries across kinds.
const kind = computed<FacetKind>({
    get: () => KIND_OPTIONS.find(o => o.value === query.tab.value)?.value ?? 'genres',
    set: v => {
        query.tab.value = v
        query.page.value = '1'
    },
})
const library = computed({
    get: () => query.library.value,
    set: v => {
        query.library.value = v
        query.page.value = '1'
    },
})
const sort = computed<Sort>(() => (query.sort.value === 'name' ? 'name' : 'count'))
const order = computed(() =>
    query.order.value === 'asc' || query.order.value === 'desc'
        ? query.order.value
        : DEFAULT_ORDER[sort.value]
)
const page = computed({
    get: () => Math.max(1, Number.parseInt(query.page.value) || 1),
    set: v => (query.page.value = String(v)),
})

function toggleSort(key: Sort) {
    if (sort.value === key) {
        query.order.value = order.value === 'asc' ? 'desc' : 'asc'
    } else {
        query.sort.value = key
        query.order.value = null
    }
    query.page.value = '1'
}

const searchInput = ref(query.q.value)
const typedSearch = refDebounced(
    computed(() => searchInput.value.trim()),
    300
)
watch(typedSearch, v => {
    query.q.value = v
    query.page.value = '1'
})
// The URL can change under the box: Back, or the sidebar link.
watch(query.q, q => {
    if (q.trim() !== searchInput.value.trim()) searchInput.value = q
})

const qLibraries = librariesApi.useList()
const libraryOptions = computed(() =>
    (qLibraries.data.value ?? []).map(l => ({ value: l.id, label: l.name }))
)

const qList = facetsApi.useList(kind, () => ({
    q: query.q.value || undefined,
    sort: sort.value,
    order: order.value,
    library_id: library.value ?? undefined,
    limit: PAGE_SIZE,
    offset: (page.value - 1) * PAGE_SIZE,
}))
const rows = computed(() => qList.data.value?.data ?? [])
const pageCount = computed(() => Math.ceil((qList.data.value?.total ?? 0) / PAGE_SIZE))
// A page past the end, from an old URL or a shrunk list, moves to the last one.
watch([pageCount, qList.isPlaceholderData], ([count, placeholder]) => {
    if (qList.data.value && !placeholder && page.value > Math.max(1, count)) {
        page.value = Math.max(1, count)
    }
})
</script>
