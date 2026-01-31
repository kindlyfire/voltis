<template>
    <VContainer>
        <div class="d-flex align-center mb-6">
            <h1 class="text-h4">My Lists</h1>
            <VSpacer />
            <VBtn color="primary" @click="showListModal('new')">Create List</VBtn>
        </div>

        <VTable>
            <thead>
                <tr>
                    <th>Name</th>
                    <th>Visibility</th>
                    <th>Entries</th>
                    <th>Updated</th>
                    <th></th>
                </tr>
            </thead>
            <tbody>
                <tr v-for="list in lists.data?.value" :key="list.id">
                    <td>
                        <RouterLink :to="`/${list.id}`" class="text-body-1 text-primary">
                            {{ list.name }}
                        </RouterLink>
                    </td>
                    <td class="text-capitalize">{{ list.visibility }}</td>
                    <td>{{ list.entry_count ?? 0 }}</td>
                    <td>{{ new Date(list.updated_at).toLocaleString() }}</td>
                    <td>
                        <VBtn
                            icon="mdi-pencil"
                            variant="text"
                            size="small"
                            @click="showListModal(list.id)"
                        />
                    </td>
                </tr>
            </tbody>
        </VTable>
    </VContainer>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { customListsApi } from '@/utils/api/custom-lists'
import { showListModal } from './ListModal.vue'

useHead({
    title: 'Lists',
})

const lists = customListsApi.useList('me')
</script>
