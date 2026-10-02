<template>
    <ADialog
        :open="open"
        :title="title"
        size="lg"
        :dismissible="phase !== 'starting'"
        @update:open="v => !v && close(phase === 'scanning')"
    >
        <div v-if="phase === 'form'" class="flex flex-col gap-4">
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

            <div
                v-if="logs.length"
                ref="log"
                role="region"
                aria-label="Scan log"
                class="bg-surface-2 rounded-field max-h-40 overflow-auto p-3 text-xs leading-snug wrap-break-word"
                tabindex="0"
            >
                <section v-for="entry in logs" :key="entry.id">
                    <h4 v-if="rows.length > 1" class="font-semibold">{{ entry.heading }}</h4>
                    <pre class="whitespace-pre-wrap">{{ entry.text }}</pre>
                </section>
            </div>
        </div>

        <template #actions>
            <template v-if="phase !== 'scanning'">
                <AButton
                    variant="text"
                    tone="neutral"
                    :disabled="phase === 'starting'"
                    @click="close()"
                >
                    Cancel
                </AButton>
                <AButton :loading="phase === 'starting'" @click="startScan">Start scan</AButton>
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
    taskIds?: string[]
    autoStart?: boolean
}>()

const store = useScanStore()
const qLibraries = librariesApi.useList()
const mScan = librariesApi.useScan()
const qMe = usersApi.useMe()
const isAdmin = computed(() => !!qMe.data.value?.permissions.includes('ADMIN'))

const forceScan = ref(!!props.contentIds?.length)
const ids = ref<string[]>(props.taskIds ?? [])
const phase = ref<'form' | 'starting' | 'scanning'>(ids.value.length ? 'scanning' : 'form')

const closeButton = useTemplateRef<{ $el: HTMLElement }>('closeButton')
const log = useTemplateRef<HTMLElement>('log')

const isContentScan = computed(() => !!props.contentIds?.length)

const rows = computed(() =>
    ids.value.flatMap(id => {
        const task = store.tasks[id]
        return task ? [scanRow(task)] : []
    })
)

const logs = computed(() =>
    ids.value.flatMap(id => {
        const text = store.logs[id]?.text.trimEnd()
        if (!text) return []
        const input = store.tasks[id]?.input
        const name = getLibraryName(input?.library_id ?? '')
        const heading = input?.filter_paths?.length ? `${name} (selected content)` : name
        return [{ id, heading, text }]
    })
)

// Follows new lines only while the log is scrolled to its end, measured before the DOM update.
watch(logs, async () => {
    const el = log.value
    const follow = !el || el.scrollHeight - el.scrollTop - el.clientHeight < 8
    await nextTick()
    if (follow && log.value) log.value.scrollTop = log.value.scrollHeight
})

function pumpLogs(): void {
    if (!props.open) return
    for (const id of ids.value) {
        const task = store.tasks[id]
        if (task && task.log_len > (store.logs[id]?.len ?? 0)) {
            void store.fetchLogs(id).catch(() => {})
        }
    }
}

watch(
    () =>
        ids.value
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
    if (props.taskIds?.length) return 'Library scans'
    if (isContentScan.value) return 'Scan content'
    if (props.libraryIds.length === 0) return 'Scan all libraries'
    if (props.libraryIds.length === 1) return `Scan ${getLibraryName(props.libraryIds[0]!)}`
    return `Scan ${props.libraryIds.length} libraries`
})

function getLibraryName(id: string): string {
    return qLibraries.data?.value?.find(l => l.id === id)?.name ?? id
}

async function startScan() {
    // Set here, not read from mScan.isPending, which turns true only after a scheduler tick.
    phase.value = 'starting'
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
    if (!res) {
        phase.value = 'form'
        return
    }
    ids.value = res.task_ids
    phase.value = 'scanning'
    void store.reconcile(res.task_ids).catch(() => {})
    // Start scan is gone: keep focus on the dialog's remaining action.
    await nextTick()
    closeButton.value?.$el.focus()
}

if (ids.value.length) void store.reconcile(ids.value).catch(() => {})
if (props.autoStart) void startScan()
</script>

<script lang="ts">
import { Modals, type ShowOptions } from '@/utils/modals'
import Self from './ScanModal.vue'

/** Resolves true once the scan started. `start` starts it without waiting for the user. */
export function showScanModal(
    libraryIds: string[],
    options?: { start?: boolean } & ShowOptions
): Promise<boolean>
export function showScanModal(
    opts: { contentIds: string[] },
    options?: ShowOptions
): Promise<boolean>
export function showScanModal(opts: { taskIds: string[] }, options?: ShowOptions): Promise<boolean>
export function showScanModal(
    arg: string[] | { contentIds: string[] } | { taskIds: string[] },
    { start, ...options }: { start?: boolean } & ShowOptions = {}
): Promise<boolean> {
    const props = Array.isArray(arg)
        ? { libraryIds: arg, autoStart: start }
        : { libraryIds: [], ...arg }
    return Modals.show<boolean | undefined>(Self, props, options).then(started => started === true)
}
</script>
