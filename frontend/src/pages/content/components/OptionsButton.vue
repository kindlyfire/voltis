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
                prepend-icon="mdi-check-all"
                title="Mark all as read"
                @click="mMarkAllRead.mutate()"
            />
            <VListItem
                prepend-icon="mdi-refresh"
                title="Reset progress"
                @click="mResetProgress.mutate()"
            />
        </VList>
    </VMenu>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { contentApi } from '@/utils/api/content'
import { usersApi } from '@/utils/api/users'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { showEditMetadataModal } from './EditMetadataModal.vue'
import { showListsModal } from './ListsModal.vue'

const props = defineProps<{
    contentId: string
}>()

const qMe = usersApi.useMe()
const isAdmin = computed(() => qMe.data.value?.permissions.includes('ADMIN'))

const queryClient = useQueryClient()

const mMarkAllRead = useMutation({
    mutationFn: async () => {
        if (confirm('Are you sure you want to mark all items as read?')) {
            await contentApi.setSeriesItemStatuses(props.contentId, 'completed')
            queryClient.invalidateQueries()
        }
    },
})

const mResetProgress = useMutation({
    mutationFn: async () => {
        if (confirm('Are you sure you want to reset your progress for this series?')) {
            await contentApi.setSeriesItemStatuses(props.contentId, null)
            queryClient.invalidateQueries()
        }
    },
})
</script>
