<template>
    <NotFoundPage v-if="notFound" />
    <div v-else class="page-frame flex flex-col gap-3.5">
        <QueryError v-if="qEntry.isError.value" :query="qEntry" />

        <div v-else-if="!entry" class="flex justify-center py-16">
            <ASpinner size="lg" />
        </div>

        <template v-else>
            <APageHeader :title="title">
                <template #actions>
                    <ASelect
                        v-model="query.library.value"
                        :options="libraryOptions"
                        label="Library"
                        placeholder="All libraries"
                        clearable
                        size="sm"
                        class="ml-2 w-full sm:w-56"
                    />
                </template>
            </APageHeader>

            <nav
                v-if="entry.roles.length || query.role.value"
                aria-label="Roles"
                class="flex flex-wrap items-center gap-1.5"
            >
                <AChip
                    v-for="chip in roleChips"
                    :key="chip.role ?? ''"
                    :to="{ query: { ...route.query, role: chip.role ?? undefined } }"
                    :tone="chip.role === query.role.value ? 'primary' : 'neutral'"
                    :aria-current="chip.role === query.role.value ? 'true' : 'false'"
                >
                    {{ chip.label }}
                </AChip>
            </nav>

            <ContentGrid
                store-key="facet"
                :params="{
                    parent_id: 'null',
                    facet_kind: kind,
                    facet: entry.key,
                    facet_role: (kind === 'people' && query.role.value) || undefined,
                    library_id: query.library.value ?? undefined,
                }"
            />
        </template>
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import ContentGrid from '@/components/ContentGrid/ContentGrid.vue'
import QueryError from '@/components/QueryError.vue'
import NotFoundPage from '@/pages/NotFoundPage.vue'
import AChip from '@/ui/AChip.vue'
import APageHeader from '@/ui/APageHeader.vue'
import ASelect from '@/ui/ASelect.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { facetsApi } from '@/utils/api/facets'
import { librariesApi } from '@/utils/api/libraries'
import type { FacetKind } from '@/utils/api/types'
import { slugLabel } from '@/utils/facets'
import { RequestError } from '@/utils/fetch'
import { useRouteQueryParams } from '@/utils/misc'

const props = defineProps<{ kind: FacetKind }>()

const route = useRoute()
const query = useRouteQueryParams({ library: null as string | null, role: null as string | null })

const qLibraries = librariesApi.useList()
const libraryOptions = computed(() =>
    (qLibraries.data.value ?? []).map(l => ({ value: l.id, label: l.name }))
)

const qEntry = facetsApi.useEntry(
    () => props.kind,
    () => route.params.key as string,
    query.library
)
const entry = computed(() => (qEntry.isError.value ? undefined : qEntry.data.value))
const notFound = computed(() => {
    const error = qEntry.error.value
    return error instanceof RequestError && error.response?.status === 404
})

const title = computed(() => {
    const name = entry.value?.name ?? ''
    return props.kind === 'genres' ? slugLabel(name) : name
})
useHead({ title: () => title.value || 'Discover' })

const roleChips = computed(() => [
    { role: null, label: `All (${entry.value?.count ?? 0})` },
    ...(entry.value?.roles ?? []).map(r => ({
        role: r.role,
        label: `${slugLabel(r.role)} (${r.count})`,
    })),
])
</script>
