<template>
    <VDialog :model-value="open" @update:model-value="v => !v && close()" max-width="500">
        <VCard>
            <VCardTitle>{{ title }}</VCardTitle>
            <VCardText>
                <template v-if="!scanning">
                    <VCheckbox
                        class="[&_.v-input__details]:-mt-[15px] [&_.v-input__details]:ml-10 [&_.v-messages__message]:leading-[15px]"
                        v-model="forceScan"
                        :disabled="isContentScan"
                        label="Force scan"
                        :messages="[
                            'Force scanning will re-scan all files, even if they have not changed since the last scan.',
                        ]"
                    />
                    <div class="mt-6 flex justify-end gap-2">
                        <VBtn variant="text" @click="close()">Cancel</VBtn>
                        <VBtn color="primary" @click="startScan">Start Scan</VBtn>
                    </div>
                </template>

                <template v-else>
                    <div v-if="!isAdmin" class="py-4 text-center">
                        Scan progress is no longer available.
                    </div>

                    <div v-else-if="rows.length === 0" class="py-4 text-center">
                        <VProgressCircular indeterminate class="mb-4" />
                        <div>Starting scan...</div>
                    </div>

                    <div v-else class="flex flex-col gap-3">
                        <div v-for="row in rows" :key="row.id" class="rounded border p-3">
                            <div class="font-medium">{{ getLibraryName(row.libraryId) }}</div>
                            <VProgressLinear
                                v-if="row.status !== TaskStatus.PENDING"
                                :model-value="row.value"
                                :indeterminate="row.indeterminate"
                                :color="row.color"
                                class="mt-2"
                                rounded
                                height="6"
                            />
                            <div
                                class="mt-1 opacity-60"
                                :class="row.status === TaskStatus.PENDING ? 'text-sm' : 'text-xs'"
                            >
                                {{ row.detail }}
                            </div>
                        </div>

                        <pre
                            v-if="logText"
                            class="max-h-40 overflow-auto rounded border p-2 text-xs whitespace-pre-wrap"
                            >{{ logText }}</pre>
                    </div>

                    <div class="mt-4 flex justify-end">
                        <VBtn variant="text" @click="close()">Close</VBtn>
                    </div>
                </template>
            </VCardText>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { scanRow, useScanStore } from '@/stores/scans'
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
    if (isContentScan.value) return 'Scan Content'
    if (props.libraryIds.length === 0) return 'Scan All Libraries'
    if (props.libraryIds.length === 1) return `Scan ${getLibraryName(props.libraryIds[0]!)}`
    return `Scan ${props.libraryIds.length} Libraries`
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
