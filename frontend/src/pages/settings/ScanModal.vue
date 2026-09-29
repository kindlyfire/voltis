<template>
    <ADialog
        :open="open"
        :title="title"
        size="lg"
        :dismissible="!mScan.isPending.value"
        @update:open="v => !v && close(scanning)"
    >
        <div v-if="!scanning" class="flex flex-col gap-4">
            <ACheckbox
                v-model="forceScan"
                :disabled="isContentScan"
                label="Force scan"
                hint="Re-scans every file, even the ones that have not changed since the last scan."
            />
            <QueryError :mutation="mScan" />
        </div>

        <p v-else-if="!isAdmin" class="text-fg-muted py-4 text-center">
            Scan progress is no longer available.
        </p>

        <div v-else-if="rows.length === 0" class="flex flex-col items-center gap-3 py-4">
            <ASpinner decorative />
            <p role="status">Starting the scan…</p>
        </div>

        <div v-else class="flex flex-col gap-3">
            <ul class="flex flex-col gap-4">
                <li v-for="row in rows" :key="row.id" class="flex flex-col gap-2">
                    <h3 class="truncate text-sm font-semibold">
                        {{ getLibraryName(row.libraryId) }}
                    </h3>
                    <ScanStrip :row="row" :label="`Scan of ${getLibraryName(row.libraryId)}`" />
                </li>
            </ul>

            <pre
                v-if="logText"
                ref="log"
                class="bg-surface-2 rounded-field max-h-40 overflow-auto p-3 text-xs leading-snug wrap-break-word whitespace-pre-wrap"
                tabindex="0"
                aria-label="Scan log"
                >{{ logText }}</pre>
        </div>

        <template #actions>
            <template v-if="!scanning">
                <AButton
                    variant="text"
                    tone="neutral"
                    :disabled="mScan.isPending.value"
                    @click="close()"
                >
                    Cancel
                </AButton>
                <AButton :loading="mScan.isPending.value" @click="startScan">Start scan</AButton>
            </template>
            <AButton v-else ref="closeButton" variant="text" tone="neutral" @click="close(true)">
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
import ASpinner from '@/ui/ASpinner.vue'
import { librariesApi } from '@/utils/api/libraries'
import { usersApi } from '@/utils/api/users'
import ScanStrip from './ScanStrip.vue'

const props = defineProps<{
    open: boolean
    close: (started?: boolean) => void
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
const log = useTemplateRef<HTMLElement>('log')

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

// Follows new lines only while the log is scrolled to its end, measured before the DOM update.
watch(logText, async () => {
    const el = log.value
    const follow = !el || el.scrollHeight - el.scrollTop - el.clientHeight < 8
    await nextTick()
    if (follow && log.value) log.value.scrollTop = log.value.scrollHeight
})

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
    const res = await mScan
        .mutateAsync(
            isContentScan.value
                ? { contentIds: props.contentIds }
                : {
                      ids: props.libraryIds.length > 0 ? props.libraryIds : undefined,
                      force: forceScan.value,
                  }
        )
        .catch(() => null) // QueryError shows it, and the form stays for a retry.
    if (!res) return
    taskIds.value = res.task_ids
    scanning.value = true
    void store.reconcile(res.task_ids).catch(() => {})
    // Start scan is gone: keep focus on the dialog's remaining action.
    await nextTick()
    closeButton.value?.$el.focus()
}
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './ScanModal.vue'

/** Resolves true once the scan started. */
export function showScanModal(libraryIds: string[]): Promise<boolean>
export function showScanModal(opts: { contentIds: string[] }): Promise<boolean>
export function showScanModal(arg: string[] | { contentIds: string[] }): Promise<boolean> {
    const props = Array.isArray(arg)
        ? { libraryIds: arg }
        : { libraryIds: [], contentIds: arg.contentIds }
    return Modals.show<boolean | undefined>(Self, props).then(started => started === true)
}
</script>
