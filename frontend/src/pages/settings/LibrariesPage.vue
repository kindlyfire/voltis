<template>
    <div class="settings-page max-w-[960px]">
        <div class="mb-1.5 flex flex-wrap items-end justify-between gap-x-4 gap-y-2">
            <APageHeader title="Libraries" />
            <div class="flex flex-wrap gap-2">
                <AButton variant="tonal" :leading-icon="IconMagnifyScan" @click="showScanModal([])">
                    Scan all
                </AButton>
                <AButton
                    ref="createButton"
                    :leading-icon="IconPlus"
                    @click="showLibraryModal('new')"
                >
                    Create library
                </AButton>
            </div>
        </div>

        <QueryError :query="libraries" />

        <ACard padding="none">
            <ATable>
                <thead>
                    <tr>
                        <th>Name</th>
                        <th class="max-sm:hidden">Type</th>
                        <th class="text-end max-sm:hidden">Top-level entries</th>
                        <th class="text-end">All entries</th>
                        <th class="max-md:hidden">Last scanned</th>
                        <th><span class="sr-only">Actions</span></th>
                    </tr>
                </thead>
                <tbody>
                    <tr v-if="libraries.isLoading.value">
                        <td colspan="6"><ASpinner class="mx-auto flex" /></td>
                    </tr>
                    <tr v-else-if="libraries.isSuccess.value && !libraries.data.value?.length">
                        <td colspan="6" class="text-fg-muted text-center">
                            No libraries yet. Create one to start scanning.
                        </td>
                    </tr>
                    <tr v-for="library in libraries.data.value" :key="library.id">
                        <td class="font-medium">{{ library.name }}</td>
                        <td class="capitalize max-sm:hidden">{{ library.type }}</td>
                        <td class="text-end max-sm:hidden">
                            {{ library.root_content_count ?? 0 }}
                        </td>
                        <td class="text-end">{{ library.content_count ?? 0 }}</td>
                        <td class="whitespace-nowrap max-md:hidden">
                            {{
                                library.scanned_at
                                    ? new Date(library.scanned_at).toLocaleString()
                                    : 'Never'
                            }}
                        </td>
                        <td class="text-end whitespace-nowrap">
                            <AIconButton
                                :icon="IconMagnifyScan"
                                :label="`Scan ${library.name}`"
                                size="sm"
                                @click="showScanModal([library.id])"
                            />
                            <AIconButton
                                :icon="IconPencil"
                                :label="`Edit ${library.name}`"
                                size="sm"
                                @click="showLibraryModal(library.id, { focusFallback })"
                            />
                            <CopyIdButton :id="library.id" :name="library.name" />
                        </td>
                    </tr>
                </tbody>
            </ATable>
        </ACard>
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { useTemplateRef } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import AIconButton from '@/ui/AIconButton.vue'
import APageHeader from '@/ui/APageHeader.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ATable from '@/ui/ATable.vue'
import { IconMagnifyScan, IconPencil, IconPlus } from '@/ui/icons'
import { librariesApi } from '@/utils/api/libraries'
import CopyIdButton from './CopyIdButton.vue'
import { showLibraryModal } from './LibraryModal.vue'
import { showScanModal } from './ScanModal.vue'

useHead({
    title: 'Libraries',
})

const libraries = librariesApi.useList()
const createButton = useTemplateRef<{ $el: HTMLElement }>('createButton')
// A deleted library's Edit button is gone.
const focusFallback = () => createButton.value?.$el
</script>
