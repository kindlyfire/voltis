<template>
    <div class="settings-page max-w-[1200px]">
        <APageHeader title="Tasks" class="mb-1.5" />

        <ACard padding="none">
            <ATable :aria-busy="tasks.isFetching.value">
                <thead>
                    <tr>
                        <th>Name</th>
                        <th>Status</th>
                        <ASortHeader
                            :sort="sort.key === 'created_at' ? sort.order : null"
                            @toggle="toggleSort('created_at')"
                        >
                            Created
                        </ASortHeader>
                        <ASortHeader
                            :sort="sort.key === 'updated_at' ? sort.order : null"
                            class="max-sm:hidden"
                            @toggle="toggleSort('updated_at')"
                        >
                            Updated
                        </ASortHeader>
                    </tr>
                </thead>
                <tbody>
                    <tr v-if="tasks.isLoading.value">
                        <td :colspan="COLUMNS"><ASpinner class="mx-auto flex" /></td>
                    </tr>
                    <tr v-else-if="tasks.isError.value">
                        <td :colspan="COLUMNS" class="py-3">
                            <QueryError :query="tasks" />
                        </td>
                    </tr>
                    <tr v-else-if="!rows.length">
                        <td :colspan="COLUMNS" class="text-fg-muted text-center">No tasks yet.</td>
                    </tr>
                    <template v-for="task in rows" :key="task.id">
                        <tr>
                            <td>
                                <AButton
                                    variant="text"
                                    tone="neutral"
                                    size="sm"
                                    class="-ml-3"
                                    :leading-icon="
                                        expanded.has(task.id) ? IconChevronDown : IconChevronRight
                                    "
                                    :aria-expanded="expanded.has(task.id)"
                                    :aria-controls="detailsId(task.id)"
                                    @click="toggleExpanded(task.id)"
                                >
                                    {{ task.name }}
                                </AButton>
                            </td>
                            <td>
                                <AChip size="sm" :tone="STATUS[task.status]?.tone ?? 'neutral'">
                                    {{ STATUS[task.status]?.label ?? 'Unknown' }}
                                </AChip>
                            </td>
                            <td class="sm:whitespace-nowrap">{{ formatDate(task.created_at) }}</td>
                            <td class="whitespace-nowrap max-sm:hidden">
                                {{ formatDate(task.updated_at) }}
                            </td>
                        </tr>
                        <tr v-if="expanded.has(task.id)" :id="detailsId(task.id)">
                            <td :colspan="COLUMNS" class="bg-surface-1 py-4">
                                <div class="grid gap-4 lg:grid-cols-2">
                                    <section class="flex min-w-0 flex-col gap-1">
                                        <h2 class="text-fg-muted text-xs font-semibold">Input</h2>
                                        <pre
                                            class="bg-surface-3 rounded-field overflow-auto p-3 text-xs leading-snug wrap-break-word whitespace-pre-wrap"
                                            >{{ formatJson(task.input) }}</pre>
                                    </section>
                                    <section class="flex min-w-0 flex-col gap-1">
                                        <h2 class="text-fg-muted text-xs font-semibold">Output</h2>
                                        <pre
                                            class="bg-surface-3 rounded-field overflow-auto p-3 text-xs leading-snug wrap-break-word whitespace-pre-wrap"
                                            >{{ formatJson(task.output) }}</pre>
                                    </section>
                                    <section
                                        v-if="task.logs"
                                        class="flex min-w-0 flex-col gap-1 lg:col-span-2"
                                    >
                                        <h2 class="text-fg-muted text-xs font-semibold">Logs</h2>
                                        <pre
                                            class="bg-surface-3 rounded-field max-h-100 overflow-auto p-3 text-xs leading-snug wrap-break-word whitespace-pre-wrap"
                                            >{{ task.logs }}</pre>
                                    </section>
                                </div>
                            </td>
                        </tr>
                    </template>
                </tbody>
            </ATable>

            <footer
                v-if="total > 0"
                class="border-outline-variant flex flex-wrap items-center justify-center gap-x-6 gap-y-2 border-t px-4 py-2 sm:justify-end"
            >
                <div class="flex items-center gap-2 text-sm">
                    <span class="text-fg-muted" aria-hidden="true">Rows per page</span>
                    <ASelect
                        :model-value="itemsPerPage"
                        :options="PAGE_SIZES"
                        label="Rows per page"
                        size="sm"
                        class="w-24"
                        @update:model-value="v => v && (itemsPerPage = v)"
                    />
                </div>
                <span class="text-fg-muted text-sm tabular-nums">
                    {{ rangeStart }}–{{ rangeEnd }} of {{ total }}
                </span>
                <APagination
                    v-model:page="page"
                    :length="pageCount"
                    label="Task pages"
                    class="max-sm:basis-full"
                />
            </footer>
        </ACard>
    </div>
</template>

<script setup lang="ts">
import { keepPreviousData } from '@tanstack/vue-query'
import { useHead } from '@unhead/vue'
import { computed, reactive, ref, watch } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import AChip from '@/ui/AChip.vue'
import APageHeader from '@/ui/APageHeader.vue'
import APagination from '@/ui/APagination.vue'
import ASelect from '@/ui/ASelect.vue'
import ASortHeader from '@/ui/ASortHeader.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ATable from '@/ui/ATable.vue'
import { IconChevronDown, IconChevronRight } from '@/ui/icons'
import { tasksApi } from '@/utils/api/tasks'
import { TaskStatus } from '@/utils/api/types'

useHead({ title: 'Tasks' })

const COLUMNS = 4
const PAGE_SIZES = [10, 25, 50, 100]

type SortKey = 'created_at' | 'updated_at'

const STATUS: Record<
    number,
    { label: string; tone: 'neutral' | 'info' | 'success' | 'danger' | 'warning' }
> = {
    [TaskStatus.PENDING]: { label: 'Pending', tone: 'neutral' },
    [TaskStatus.IN_PROGRESS]: { label: 'In progress', tone: 'info' },
    [TaskStatus.COMPLETED]: { label: 'Completed', tone: 'success' },
    [TaskStatus.FAILED]: { label: 'Failed', tone: 'danger' },
    [TaskStatus.CANCELLED]: { label: 'Cancelled', tone: 'warning' },
}

const itemsPerPage = ref(10)
const page = ref(1)
const sort = ref<{ key: SortKey; order: 'asc' | 'desc' }>({ key: 'created_at', order: 'desc' })
const expanded = reactive(new Set<string>())

const params = computed(() => ({
    limit: itemsPerPage.value,
    offset: (page.value - 1) * itemsPerPage.value,
    sort: sort.value.key,
    sort_order: sort.value.order,
}))

const tasks = tasksApi.useList(params, { placeholderData: keepPreviousData })
const rows = computed(() => tasks.data.value?.data ?? [])
const total = computed(() => tasks.data.value?.total ?? 0)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / itemsPerPage.value)))
const rangeStart = computed(() => (rows.value.length ? params.value.offset + 1 : 0))
const rangeEnd = computed(() => params.value.offset + rows.value.length)

watch([sort, itemsPerPage], () => (page.value = 1))

function toggleSort(key: SortKey) {
    sort.value =
        sort.value.key === key
            ? { key, order: sort.value.order === 'asc' ? 'desc' : 'asc' }
            : { key, order: 'desc' }
}

function toggleExpanded(id: string) {
    if (!expanded.delete(id)) expanded.add(id)
}

function detailsId(id: string) {
    return `task-${id}-details`
}

function formatDate(value: string) {
    return new Date(value).toLocaleString()
}

function formatJson(data: unknown) {
    return JSON.stringify(data, null, 2)
}
</script>
