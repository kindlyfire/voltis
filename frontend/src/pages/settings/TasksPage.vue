<template>
    <VContainer>
        <h1 class="mb-6 text-3xl">Tasks</h1>

        <VDataTableServer
            v-model:items-per-page="itemsPerPage"
            v-model:page="page"
            v-model:sort-by="sortBy"
            :headers="headers"
            :items="tasks.data.value?.data ?? []"
            :items-length="tasks.data.value?.total ?? 0"
            :loading="tasks.isLoading.value"
            item-value="id"
            show-expand
            expand-on-click
        >
            <template #item.status="{ value }">
                <VChip :color="statusColor(value)" size="small">
                    {{ statusLabel(value) }}
                </VChip>
            </template>

            <template #item.created_at="{ value }">
                {{ new Date(value).toLocaleString() }}
            </template>

            <template #item.updated_at="{ value }">
                {{ new Date(value).toLocaleString() }}
            </template>

            <template #expanded-row="{ columns, item }">
                <tr class="bg-neutral-50 shadow-inner">
                    <td :colspan="columns.length" class="p-4">
                        <div class="flex flex-col gap-4">
                            <div>
                                <div class="mb-1 text-sm font-semibold">Input</div>
                                <pre
                                    class="bg-surface-variant overflow-auto rounded p-3 font-mono text-sm"
                                    >{{ formatJson(item.input) }}</pre
                                >
                            </div>
                            <div>
                                <div class="mb-1 text-sm font-semibold">Output</div>
                                <pre
                                    class="bg-surface-variant overflow-auto rounded p-3 font-mono text-sm"
                                    >{{ formatJson(item.output) }}</pre
                                >
                            </div>
                            <div v-if="item.logs">
                                <div class="mb-1 text-sm font-semibold">Logs</div>
                                <pre
                                    class="bg-surface-variant max-h-100 overflow-auto rounded p-3 font-mono text-sm"
                                    >{{ item.logs }}</pre
                                >
                            </div>
                        </div>
                    </td>
                </tr>
            </template>
        </VDataTableServer>
    </VContainer>
</template>

<script setup lang="ts">
import { keepPreviousData } from '@tanstack/vue-query'
import { useHead } from '@unhead/vue'
import { computed, ref } from 'vue'
import { tasksApi } from '@/utils/api/tasks'
import { TaskStatus } from '@/utils/api/types'

useHead({ title: 'Tasks' })

const itemsPerPage = ref(10)
const page = ref(1)
const sortBy = ref<{ key: string; order: 'asc' | 'desc' }[]>([{ key: 'created_at', order: 'desc' }])

const headers = [
    { title: 'Name', key: 'name', sortable: false },
    { title: 'Status', key: 'status', sortable: false },
    { title: 'Created', key: 'created_at', sortable: true },
    { title: 'Updated', key: 'updated_at', sortable: true },
]

const params = computed(() => ({
    limit: itemsPerPage.value,
    offset: (page.value - 1) * itemsPerPage.value,
    sort: (sortBy.value[0]?.key as 'created_at' | 'updated_at') ?? 'created_at',
    sort_order: sortBy.value[0]?.order ?? ('desc' as const),
}))

const tasks = tasksApi.useList(params, { placeholderData: keepPreviousData })

function statusColor(status: number) {
    switch (status) {
        case TaskStatus.IN_PROGRESS:
            return 'blue'
        case TaskStatus.COMPLETED:
            return 'green'
        case TaskStatus.FAILED:
            return 'red'
        default:
            return 'grey'
    }
}

function statusLabel(status: number) {
    switch (status) {
        case TaskStatus.IN_PROGRESS:
            return 'In Progress'
        case TaskStatus.COMPLETED:
            return 'Completed'
        case TaskStatus.FAILED:
            return 'Failed'
        default:
            return 'Unknown'
    }
}

function formatJson(data: unknown) {
    return JSON.stringify(data, null, 2)
}
</script>
