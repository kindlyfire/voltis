<template>
    <VContainer>
        <div class="d-flex align-center mb-6">
            <h1 class="text-h4">Libraries</h1>
            <VSpacer />
            <VBtn color="primary" @click="showLibraryModal('new')">Create Library</VBtn>
        </div>

        <VTable>
            <thead>
                <tr>
                    <th>Name</th>
                    <th>Type</th>
                    <th>Library Entries</th>
                    <th>Last Scanned</th>
                    <th></th>
                </tr>
            </thead>
            <tbody>
                <tr v-for="library in libraries.data?.value" :key="library.id">
                    <td>{{ library.name }}</td>
                    <td>{{ library.type }}</td>
                    <td>
                        <abbr title="Without/with children">
                            {{ library.root_content_count ?? 0 }}
                            / {{ library.content_count ?? 0 }}
                        </abbr>
                    </td>
                    <td>
                        {{
                            library.scanned_at
                                ? new Date(library.scanned_at + 'Z').toLocaleString()
                                : 'Never'
                        }}
                    </td>
                    <td>
                        <VBtn
                            icon="mdi-magnify-scan"
                            variant="text"
                            size="small"
                            @click="showScanModal([library.id])"
                            title="Scan library"
                        />
                        <VBtn
                            icon="mdi-pencil"
                            variant="text"
                            size="small"
                            @click="showLibraryModal(library.id)"
                            title="Edit library"
                        />
                        <CopyIdButton :id="library.id" />
                    </td>
                </tr>
            </tbody>
        </VTable>

        <div class="mt-4">
            <VBtn variant="tonal" prepend-icon="mdi-magnify-scan" @click="showScanModal([])">
                Scan All
            </VBtn>
        </div>
    </VContainer>
</template>

<script setup lang="ts">
import { librariesApi } from '@/utils/api/libraries'
import { showLibraryModal } from './LibraryModal.vue'
import { showScanModal } from './ScanModal.vue'
import CopyIdButton from './CopyIdButton.vue'
import { useHead } from '@unhead/vue'

useHead({
    title: 'Libraries',
})

const libraries = librariesApi.useList()
</script>
