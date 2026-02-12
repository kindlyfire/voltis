<template>
    <VMenu :offset="6">
        <template #activator="{ props: menuProps }">
            <VBtn
                variant="tonal"
                v-bind="menuProps"
                size="large"
                class="h-12! aspect-square! min-w-auto!"
            >
                <VIcon>mdi-dots-vertical</VIcon>
            </VBtn>
        </template>
        <VList>
            <VListItem
                prepend-icon="mdi-download"
                title="Download"
                @click="showDownloadModal(props.contentId)"
            />
            <VListItem
                prepend-icon="mdi-format-list-bulleted"
                title="Add to list"
                @click="showListsModal(props.contentId)"
            />
            <VListItem
                v-if="isAdmin"
                prepend-icon="mdi-pencil"
                title="Edit metadata"
                @click="showEditMetadataModal(props.contentId)"
            />
            <VListItem
                prepend-icon="mdi-book-sync"
                title="Update progress"
                @click="showUpdateProgressModal(props.contentId)"
            />
        </VList>
    </VMenu>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { usersApi } from '@/utils/api/users'
import { showDownloadModal } from './DownloadModal.vue'
import { showEditMetadataModal } from './EditMetadataModal.vue'
import { showListsModal } from './ListsModal.vue'
import { showUpdateProgressModal } from './UpdateProgressModal.vue'

const props = defineProps<{
    contentId: string
}>()

const qMe = usersApi.useMe()
const isAdmin = computed(() => qMe.data.value?.permissions.includes('ADMIN'))
</script>
