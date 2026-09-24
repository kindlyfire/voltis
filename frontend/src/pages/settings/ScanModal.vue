<template>
    <ADialog :open="open" :title="title" @update:open="v => !v && close()">
        <ACheckbox
            v-if="!scanning"
            v-model="forceScan"
            :disabled="isContentScan"
            label="Force scan"
            hint="Re-scans every file, even the ones that have not changed since the last scan."
        />

        <p v-else-if="!isAdmin" class="text-fg-muted py-4 text-center">
            Scan progress is no longer available.
        </p>

        <QueryError v-else-if="mScan.isError.value" :mutation="mScan" />

        <div v-else-if="rows.length === 0" class="flex flex-col items-center gap-3 py-4">
            <ASpinner decorative />
            <p role="status">Starting the scan…</p>
        </div>

        <div v-else class="flex flex-col gap-3">
            <ul class="flex flex-col gap-3">
                <li
                    v-for="row in rows"
                    :key="row.id"
                    class="border-outline-variant flex flex-col gap-2 rounded-xl border p-3"
                >
                    <div class="flex items-center gap-2 text-sm font-semibold">
                        <span class="min-w-0 truncate">{{ getLibraryName(row.libraryId) }}</span>
                        <AIcon
                            v-if="row.status >= TaskStatus.COMPLETED"
                            :icon="
                                row.status === TaskStatus.COMPLETED ? IconCheck : IconAlertCircle
                            "
                            :label="row.status === TaskStatus.COMPLETED ? 'Done' : 'Failed'"
                            :class="row.tone === 'success' ? 'text-success' : 'text-error'"
                            class="text-lg"
                        />
                    </div>
                    <AProgressBar
                        v-if="row.status !== TaskStatus.PENDING"
                        :value="row.value / 100"
                        :indeterminate="row.indeterminate"
                        :tone="row.tone"
                        :label="`Scan of ${getLibraryName(row.libraryId)}`"
                        :thickness="6"
                    />
                    <div class="text-fg-muted text-xs">{{ row.detail }}</div>
                </li>
            </ul>

            <pre
                v-if="logText"
                class="bg-surface-2 rounded-field max-h-40 overflow-auto p-3 text-xs leading-snug wrap-break-word whitespace-pre-wrap"
                tabindex="0"
                aria-label="Scan log"
                >{{ logText }}</pre>
        </div>

        <template #actions>
            <template v-if="!scanning">
                <AButton variant="text" tone="neutral" @click="close()">Cancel</AButton>
                <AButton :loading="mScan.isPending.value" @click="startScan">Start scan</AButton>
            </template>
            <AButton v-else ref="closeButton" variant="text" tone="neutral" @click="close()">
                Close
            </AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef, watch } from 'vue'
import QueryError from '@/components/QueryError.vue'
import { scanRow, useScanStore } from '@/stores/scans'
import AButton from '@/ui/AButton.vue'
import ACheckbox from '@/ui/ACheckbox.vue'
import ADialog from '@/ui/ADialog.vue'
import AIcon from '@/ui/AIcon.vue'
import AProgressBar from '@/ui/AProgressBar.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { IconAlertCircle, IconCheck } from '@/ui/icons'
import { librariesApi } from '@/utils/api/libraries'
import { TaskStatus } from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'

const props = defineProps<{
    open: boolean
    close: () => void
    libraryIds: string[]
    contentIds?: string[]
}>()

const store = useScanStore()
const qLibraries = librariesApi.useList()
const mScan = librariesApi.useScan()
const qMe = usersApi.useMe()
const isAdmin = computed(() => !!qMe.data.value?.permissions.includes('ADMIN'))

const forceScan = ref(!!props.contentIds?.length)
const scanning = ref(false)
const taskIds = ref<string[]>([])

const closeButton = useTemplateRef<{ $el: HTMLElement }>('closeButton')

const isContentScan = computed(() => !!props.contentIds?.length)

const rows = computed(() =>
    taskIds.value.flatMap(id => {
        const task = store.tasks[id]
        return task ? [scanRow(task)] : []
    })
)

const logText = computed(() =>
    taskIds.value
        .map(id => store.logs[id]?.text ?? '')
        .filter(Boolean)
        .join('\n')
        .trimEnd()
)

function pumpLogs(): void {
    if (!props.open) return
    for (const id of taskIds.value) {
        const task = store.tasks[id]
        if (task && task.log_len > (store.logs[id]?.len ?? 0)) {
            void store.fetchLogs(id).catch(() => {})
        }
    }
}

watch(
    () =>
        taskIds.value
            .map(id => `${store.tasks[id]?.log_len ?? 0}:${store.logs[id]?.len ?? 0}`)
            .join('|'),
    pumpLogs,
    { immediate: true }
)

watch(
    () => props.open,
    (open, _, onCleanup) => {
        if (!open) return
        const poll = setInterval(pumpLogs, 1000)
        onCleanup(() => clearInterval(poll))
    },
    { immediate: true }
)

const title = computed(() => {
    if (isContentScan.value) return 'Scan content'
    if (props.libraryIds.length === 0) return 'Scan all libraries'
    if (props.libraryIds.length === 1) return `Scan ${getLibraryName(props.libraryIds[0]!)}`
    return `Scan ${props.libraryIds.length} libraries`
})

function getLibraryName(id: string): string {
    return qLibraries.data?.value?.find(l => l.id === id)?.name ?? id
}

async function startScan() {
    try {
        const res = await mScan.mutateAsync(
            isContentScan.value
                ? { contentIds: props.contentIds }
                : {
                      ids: props.libraryIds.length > 0 ? props.libraryIds : undefined,
                      force: forceScan.value,
                  }
        )
        taskIds.value = res.task_ids
        void store.reconcile(res.task_ids).catch(() => {})
    } finally {
        scanning.value = true
        // Start scan is gone: keep focus on the dialog's remaining action.
        await nextTick()
        closeButton.value?.$el.focus()
    }
}
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './ScanModal.vue'

export function showScanModal(libraryIds: string[]): Promise<void>
export function showScanModal(opts: { contentIds: string[] }): Promise<void>
export function showScanModal(arg: string[] | { contentIds: string[] }): Promise<void> {
    if (Array.isArray(arg)) {
        return Modals.show(Self, { libraryIds: arg })
    }
    return Modals.show(Self, { libraryIds: [], contentIds: arg.contentIds })
}
</script>
