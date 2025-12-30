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
                @click="showListsModal = true"
            />
            <VListItem
                prepend-icon="mdi-refresh"
                title="Reset progress"
                @click="mResetProgress.mutate()"
            />
        </VList>
    </VMenu>

    <ListsModal v-model="showListsModal" :content-id="contentId" />
</template>

<script setup lang="ts">
import { contentApi } from '@/utils/api/content'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { ref } from 'vue'
import ListsModal from './ListsModal.vue'

const props = defineProps<{
    contentId: string
}>()

const queryClient = useQueryClient()
const showListsModal = ref(false)

const mResetProgress = useMutation({
    mutationFn: async () => {
        if (confirm('Are you sure you want to reset your progress for this series?')) {
            await contentApi.resetSeriesProgress(props.contentId)
            queryClient.invalidateQueries()
        }
    },
})
</script>
