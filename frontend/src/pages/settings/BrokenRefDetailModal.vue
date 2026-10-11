<template>
    <ADialog :open="open" title="Broken items" size="xl" @update:open="v => !v && close()">
        <div class="flex flex-col gap-4">
            <ATable v-if="group.items.length" density="compact">
                <thead>
                    <tr>
                        <th>URI</th>
                        <th>Data</th>
                        <th>Last read</th>
                    </tr>
                </thead>
                <tbody>
                    <template v-for="item in group.items" :key="item.id">
                        <tr>
                            <td>
                                <AButton
                                    variant="text"
                                    tone="neutral"
                                    size="sm"
                                    class="-ml-3 text-left"
                                    :leading-icon="
                                        expanded.has(item.id) ? IconChevronDown : IconChevronRight
                                    "
                                    :aria-expanded="expanded.has(item.id)"
                                    :aria-controls="`${baseId}-${item.id}`"
                                    @click="toggle(item.id)"
                                >
                                    <code class="[overflow-wrap:anywhere]">{{ item.uri }}</code>
                                </AButton>
                            </td>
                            <td class="text-fg-muted">{{ summarize(item) }}</td>
                            <td class="text-fg-muted whitespace-nowrap">
                                {{
                                    item.last_read_at
                                        ? new Date(item.last_read_at).toLocaleDateString()
                                        : '—'
                                }}
                            </td>
                        </tr>
                        <tr v-if="expanded.has(item.id)" :id="`${baseId}-${item.id}`">
                            <td colspan="3" class="bg-surface-1 py-2">
                                <pre
                                    class="bg-surface-2 rounded-field max-h-[40dvh] overflow-auto p-3 text-xs leading-snug wrap-break-word whitespace-pre-wrap"
                                    tabindex="0"
                                    :aria-label="`Data for ${item.uri}`"
                                    >{{ JSON.stringify(item, null, 2) }}</pre>
                            </td>
                        </tr>
                    </template>
                </tbody>
            </ATable>
            <section v-if="group.list_entries.length" class="flex flex-col gap-1">
                <h3 class="text-fg-muted text-xs font-semibold">List entries</h3>
                <ATable density="compact">
                    <thead>
                        <tr>
                            <th>List</th>
                            <th>URI</th>
                            <th>Notes</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr v-for="entry in group.list_entries" :key="entry.id">
                            <td>{{ entry.list_name }}</td>
                            <td>
                                <code class="[overflow-wrap:anywhere]">{{ entry.uri }}</code>
                            </td>
                            <td class="text-fg-muted">{{ entry.notes || '—' }}</td>
                        </tr>
                    </tbody>
                </ATable>
            </section>
        </div>
        <template #actions>
            <AButton variant="text" tone="neutral" autofocus @click="close()">Close</AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { reactive, useId } from 'vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import ATable from '@/ui/ATable.vue'
import { IconChevronDown, IconChevronRight } from '@/ui/icons'
import type { BrokenRefGroup } from '@/utils/api/types'
import { summarize } from './brokenRefs'

defineProps<{
    open: boolean
    close: () => void
    group: BrokenRefGroup
}>()

const baseId = useId()
const expanded = reactive(new Set<string>())

function toggle(id: string) {
    if (expanded.has(id)) expanded.delete(id)
    else expanded.add(id)
}
</script>

<script lang="ts">
import type { BrokenRefGroup as Group } from '@/utils/api/types'
import { Modals } from '@/utils/modals'
import Self from './BrokenRefDetailModal.vue'

export function showBrokenRefDetailModal(group: Group): Promise<void> {
    return Modals.show<void>(Self, { group })
}
</script>
