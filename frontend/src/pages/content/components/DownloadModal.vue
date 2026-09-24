<template>
    <ADialog :open="open" title="Download" size="sm" @update:open="v => !v && close()">
        <div class="flex flex-col gap-4">
            <div v-if="qDownloadInfo.isLoading.value" class="flex justify-center py-4">
                <ASpinner label="Estimating size" />
            </div>
            <QueryError :query="qDownloadInfo" />
            <p v-if="qDownloadInfo.data.value" class="text-fg-muted">
                <template v-if="qDownloadInfo.data.value.file_count === 1">
                    Estimate: {{ formatBytes(qDownloadInfo.data.value.total_size) }}
                </template>
                <template v-else>
                    Estimate: {{ qDownloadInfo.data.value.file_count }} files for a total of
                    {{ formatBytes(qDownloadInfo.data.value.total_size) }}
                </template>
            </p>
        </div>
        <template #actions>
            <AButton variant="text" tone="neutral" @click="close()">Cancel</AButton>
            <AButton
                :leading-icon="IconDownload"
                :disabled="!qDownloadInfo.data.value"
                @click="startDownload"
            >
                Download
            </AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { IconDownload } from '@/ui/icons'
import { contentApi } from '@/utils/api/content'
import { API_URL } from '@/utils/fetch'
import { formatBytes } from '@/utils/format'

const props = defineProps<{
    open: boolean
    close: () => void
    contentId: string
}>()

const qDownloadInfo = contentApi.useDownloadInfo(() => props.contentId)

function startDownload() {
    const a = document.createElement('a')
    a.href = `${API_URL}/files/download/${props.contentId}`
    a.click()
    props.close()
}
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './DownloadModal.vue'

export function showDownloadModal(contentId: string): Promise<void> {
    return Modals.show(Self, { contentId })
}
</script>
