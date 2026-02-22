<template>
    <VDialog :model-value="open" @update:model-value="v => !v && close()" max-width="500">
        <VCard>
            <VCardTitle>{{ title }}</VCardTitle>
            <VCardText>
                <!-- Form -->
                <template v-if="!scanning">
                    <VCheckbox
                        class="force-scan-checkbox"
                        v-model="forceScan"
                        :disabled="isContentScan"
                        label="Force scan"
                        :messages="[
                            'Force scanning will re-scan all files, even if they have not changed since the last scan.',
                        ]"
                    />
                    <div class="flex justify-end mt-6 gap-2">
                        <VBtn variant="text" @click="close()">Cancel</VBtn>
                        <VBtn color="primary" @click="startScan">Start Scan</VBtn>
                    </div>
                </template>

                <!-- Scanning -->
                <template v-else>
                    <div v-if="scans.length === 0 && !scanComplete" class="text-center py-4">
                        <VProgressCircular indeterminate class="mb-4" />
                        <div>Starting scan...</div>
                    </div>

                    <template v-else>
                        <div class="space-y-3!">
                            <div
                                v-for="item in scans"
                                :key="item.libraryId"
                                class="border rounded pa-3"
                            >
                                <div class="font-medium">{{ getLibraryName(item.libraryId) }}</div>
                                <template
                                    v-if="
                                        item.status === 'running' ||
                                        item.status === 'completed' ||
                                        item.status === 'failed'
                                    "
                                >
                                    <VProgressLinear
                                        :model-value="
                                            !item.progress
                                                ? 0
                                                : item.progress.total === 0
                                                  ? 100
                                                  : (item.progress.processed /
                                                        item.progress.total) *
                                                    100
                                        "
                                        :color="
                                            item.status === 'completed'
                                                ? 'success'
                                                : item.status === 'failed'
                                                  ? 'error'
                                                  : undefined
                                        "
                                        class="mt-2"
                                        rounded
                                        height="6"
                                    />
                                    <div class="text-xs text-medium-emphasis mt-1">
                                        {{ item.progress?.processed ?? 0 }} /
                                        {{ item.progress?.total ?? '?' }}
                                    </div>
                                    <div
                                        v-if="item.output"
                                        class="text-sm text-medium-emphasis mt-1"
                                    >
                                        {{ item.output.to_add }} to add,
                                        {{ item.output.to_update }} to update,
                                        {{ item.output.to_remove }} to remove
                                    </div>
                                </template>
                                <div v-else class="text-sm text-medium-emphasis mt-1">Queued</div>
                            </div>
                        </div>
                    </template>

                    <div class="flex justify-end mt-4">
                        <VBtn variant="text" @click="close()">Close</VBtn>
                    </div>
                </template>
            </VCardText>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { contentApi } from '@/utils/api/content'
import { librariesApi } from '@/utils/api/libraries'
import { useScanTracker } from '@/utils/ws'

const props = defineProps<{
    open: boolean
    close: () => void
    libraryIds: string[]
    contentId?: string
}>()

const libraries = librariesApi.useList()
const scan = librariesApi.useScan()
const forceScan = ref(!!props.contentId)
const scanning = ref(false)
const { scans } = useScanTracker()

const isContentScan = computed(() => !!props.contentId)
const scanComplete = computed(
    () =>
        scans.value.length > 0 &&
        scans.value.every(i => i.status === 'completed' || i.status === 'failed')
)

const title = computed(() => {
    if (isContentScan.value) return 'Scan Content'
    if (props.libraryIds.length === 0) return 'Scan All Libraries'
    if (props.libraryIds.length === 1) return `Scan ${getLibraryName(props.libraryIds[0]!)}`
    return `Scan ${props.libraryIds.length} Libraries`
})

function getLibraryName(id: string): string {
    return libraries.data?.value?.find(l => l.id === id)?.name ?? id
}

async function startScan() {
    if (isContentScan.value) {
        await contentApi.scanContent(props.contentId!)
        scanning.value = true
    } else {
        scan.mutate(
            {
                ids: props.libraryIds.length > 0 ? props.libraryIds : undefined,
                force: forceScan.value,
            },
            {
                onSuccess: () => {
                    scanning.value = true
                },
            }
        )
    }
}
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './ScanModal.vue'

export function showScanModal(libraryIds: string[]): Promise<void>
export function showScanModal(opts: { contentId: string }): Promise<void>
export function showScanModal(arg: string[] | { contentId: string }): Promise<void> {
    if (Array.isArray(arg)) {
        return Modals.show(Self, { libraryIds: arg })
    }
    return Modals.show(Self, { libraryIds: [], contentId: arg.contentId })
}
</script>

<style lang="css" scoped>
:deep(.force-scan-checkbox .v-input__details) {
    margin-left: 40px;
    margin-top: -15px;
}

:deep(.force-scan-checkbox .v-messages__message) {
    line-height: 15px;
}
</style>
