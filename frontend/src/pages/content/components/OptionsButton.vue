<template>
    <AMenu align="end">
        <template #trigger>
            <AIconButton :icon="IconDotsVertical" label="More options" variant="tonal" />
        </template>
        <AMenuItem :leading-icon="IconDownload" @select="showDownloadModal(props.contentId)">
            Download
        </AMenuItem>
        <AMenuItem :leading-icon="IconPlaylistAdd" @select="showListsModal(props.contentId)">
            Add to list
        </AMenuItem>
        <AMenuItem
            v-if="isSeries"
            :leading-icon="IconBookSync"
            @select="showUpdateProgressModal(props.contentId)"
        >
            Update progress
        </AMenuItem>
        <AMenuItem
            v-else
            :leading-icon="IconRestart"
            @select="showClearReadingModal(props.contentId)"
        >
            Clear status and position
        </AMenuItem>
        <template v-if="isAdmin">
            <AMenuSeparator />
            <AMenuItem :leading-icon="IconPencil" @select="showEditMetadataModal(props.contentId)">
                Edit metadata
            </AMenuItem>
            <AMenuItem
                :leading-icon="IconMagnifyScan"
                @select="showScanModal({ contentIds: [props.contentId] })"
            >
                Scan
            </AMenuItem>
        </template>
    </AMenu>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { showScanModal } from '@/pages/settings/ScanModal.vue'
import AIconButton from '@/ui/AIconButton.vue'
import AMenu from '@/ui/AMenu.vue'
import AMenuItem from '@/ui/AMenuItem.vue'
import AMenuSeparator from '@/ui/AMenuSeparator.vue'
import {
    IconBookSync,
    IconDotsVertical,
    IconDownload,
    IconMagnifyScan,
    IconPencil,
    IconPlaylistAdd,
    IconRestart,
} from '@/ui/icons'
import { contentApi } from '@/utils/api/content'
import { usersApi } from '@/utils/api/users'
import { showClearReadingModal } from './ClearReadingModal.vue'
import { showDownloadModal } from './DownloadModal.vue'
import { showEditMetadataModal } from './EditMetadataModal.vue'
import { showListsModal } from './ListsModal.vue'
import { showUpdateProgressModal } from './UpdateProgressModal.vue'

const props = defineProps<{
    contentId: string
}>()

const qMe = usersApi.useMe()
const isAdmin = computed(() => qMe.data.value?.permissions.includes('ADMIN'))
const qContent = contentApi.useGet(() => props.contentId)
const isSeries = computed(() => qContent.data.value?.type.includes('series'))
</script>
