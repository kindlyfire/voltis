<template>
    <VDialog :model-value="open" @update:model-value="v => !v && close()" max-width="400">
        <VCard>
            <VCardTitle>Download</VCardTitle>
            <VCardText>
                <div v-if="qDownloadInfo.isLoading.value">Loading...</div>
                <AQueryError :query="qDownloadInfo" />
                <div v-if="qDownloadInfo.data.value">
                    <span v-if="qDownloadInfo.data.value.file_count === 1">
                        Estimate: {{ formatBytes(qDownloadInfo.data.value.total_size) }}
                    </span>
                    <span v-else>
                        Estimate: {{ qDownloadInfo.data.value.file_count }} files for a total of
                        {{ formatBytes(qDownloadInfo.data.value.total_size) }}
                    </span>
                </div>
            </VCardText>

            <VCardActions>
                <VSpacer />
                <VBtn variant="text" @click="close()">Cancel</VBtn>
                <VBtn color="primary" :disabled="!qDownloadInfo.data.value" @click="startDownload">
                    Download
                </VBtn>
            </VCardActions>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
import { contentApi } from '@/utils/api/content'
import { API_URL } from '@/utils/fetch'
import { formatBytes } from '@/utils/format'
import AQueryError from '@/components/AQueryError.vue'

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
