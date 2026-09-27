<template>
    <section class="flex flex-col gap-4" aria-labelledby="orphans-heading">
        <h2 id="orphans-heading" class="font-display mt-6 text-2xl font-semibold">
            Orphaned metadata
        </h2>
        <p class="text-fg-muted max-w-[700px] text-sm leading-normal">
            Metadata overrides and provider links are kept when their content disappears. Move them
            to content that replaced it, or delete them. A move never overwrites the destination's
            own overrides or decided links.
        </p>

        <QueryError :query="qSummary" />

        <p
            v-if="qSummary.isSuccess.value && !libraryOptions.length"
            ref="emptyState"
            tabindex="-1"
            class="a-focus text-fg-muted text-sm"
        >
            No orphaned metadata.
        </p>

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
                    label="Search URIs"
                    type="search"
                    :leading-icon="IconMagnify"
                    clearable
                    class="w-full sm:w-64"
                />
                <AButton
                    :disabled="edits.size === 0"
                    focusable-when-disabled
                    :loading="mSave.isPending.value"
                    class="sm:ml-auto"
                    @click="mSave.mutate()"
                >
                    Save
                </AButton>
            </div>

            <QueryError :mutation="mSave" closable />
            <QueryError :query="qOrphans" />

            <ACard padding="none">
                <ATable :aria-busy="qOrphans.isFetching.value">
                    <thead>
                        <tr>
                            <th>URI</th>
                            <th>Kept</th>
                            <th class="w-1/2">Action</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr v-if="qOrphans.isLoading.value">
                            <td colspan="3"><ASpinner class="mx-auto flex" /></td>
                        </tr>
                        <tr v-else-if="qOrphans.isSuccess.value && !items.length">
                            <td colspan="3" class="text-fg-muted text-center">
                                {{ search ? 'No URIs match the search.' : 'No orphaned metadata.' }}
                            </td>
                        </tr>
                        <tr v-for="item in items" :key="item.uri">
                            <td class="min-w-48 py-2">
                                <code
                                    class="text-[13px] [overflow-wrap:anywhere]"
                                    :class="{ 'line-through': edits.get(item.uri) === 'delete' }"
                                    >{{ item.uri }}</code
                                >
                                <div v-if="item.title" class="text-fg-muted text-sm">
                                    {{ item.title }}
                                </div>
                            </td>
                            <td class="min-w-36 py-2">
                                <ul class="text-fg-muted text-sm">
                                    <li v-if="item.overrides.length">
                                        Overrides: {{ item.overrides.map(fieldLabel).join(', ') }}
                                    </li>
                                    <li v-for="link in item.links" :key="link.provider">
                                        {{ describeLink(link) }}
                                    </li>
                                </ul>
                            </td>
                            <td class="py-2">
                                <div class="flex items-start gap-1">
                                    <OrphanTargetField
                                        :model-value="target(item.uri)"
                                        :library-id="selectedLibraryId!"
                                        :series="item.links.length > 0"
                                        :label="`Move ${item.uri} to`"
                                        :disabled="edits.get(item.uri) === 'delete'"
                                        class="min-w-64 flex-1"
                                        @update:model-value="
                                            v => setTarget(item.uri, v ?? undefined)
                                        "
                                    />
                                    <AIconButton
                                        :icon="IconDelete"
                                        :label="`Delete the metadata of ${item.uri}`"
                                        size="sm"
                                        :pressed="edits.get(item.uri) === 'delete'"
                                        class="mt-1"
                                        @click="toggleDelete(item.uri)"
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
                    <APagination
                        v-model:page="page"
                        :length="pageCount"
                        label="Orphaned metadata pages"
                    />
                </footer>
            </ACard>
        </template>
    </section>
</template>

<script setup lang="ts">
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { computed, nextTick, useTemplateRef } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import AIconButton from '@/ui/AIconButton.vue'
import APagination from '@/ui/APagination.vue'
import ASelect from '@/ui/ASelect.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ATable from '@/ui/ATable.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconDelete, IconMagnify } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { refetchCatalog } from '@/utils/api/catalog'
import { contentApi } from '@/utils/api/content'
import { metadataApi } from '@/utils/api/metadata'
import type { OrphanedMetadata } from '@/utils/api/types'
import { plural } from '@/utils/misc'
import OrphanTargetField from './OrphanTargetField.vue'
import { useRepairTable } from './useRepairTable'

const queryClient = useQueryClient()
const toast = useToast()
const qSummary = contentApi.useOrphansSummary()
const qConfig = metadataApi.useConfig()
const emptyState = useTemplateRef('emptyState')

const {
    selectedLibraryId,
    libraryOptions,
    searchInput,
    search,
    page,
    pageCount,
    query: qOrphans,
    edits,
    target,
    setTarget,
    toggleDelete,
    changes,
} = useRepairTable(() => qSummary.data.value, contentApi.useOrphans)
const items = computed(() => qOrphans.data.value?.data ?? [])

function fieldLabel(key: string): string {
    return qConfig.data.value?.fields.find(f => f.key === key)?.label ?? key
}

function describeLink(link: OrphanedMetadata['links'][number]): string {
    const label = qConfig.data.value?.providers.find(p => p.name === link.provider)?.label
    const parts = [`${label ?? link.provider}: ${link.state}`]
    if (link.external_id) parts.push(link.external_id)
    if (link.rejected.length) parts.push(`${link.rejected.length} rejected`)
    return parts.join(', ')
}

const mSave = useMutation({
    mutationFn: async () => {
        if (!selectedLibraryId.value) return
        const { deletes, targets } = changes()
        await contentApi.fixOrphans(selectedLibraryId.value, { delete: deletes, move: targets })
    },
    onSuccess: async () => {
        toast.show({ message: `Saved ${plural(edits.size, 'change', 'changes')}` })
        edits.clear()
        // Moves change the destinations' metadata too.
        await refetchCatalog(queryClient)
        // Saving the last orphans removes the table, and the focused button with it.
        await nextTick()
        emptyState.value?.focus()
    },
})
</script>
