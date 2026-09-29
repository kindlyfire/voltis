<template>
    <AButton :leading-icon="IconBookOpen" :loading="readingStatus == null" @click="onClick">
        {{ resume ? 'Continue reading' : 'Start reading' }}
    </AButton>
</template>

<script setup lang="ts">
import { useKeyModifier } from '@vueuse/core'
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { isBookLocator } from '@/pages/read/BookDisplay/bookEntry'
import AButton from '@/ui/AButton.vue'
import { IconBookOpen } from '@/ui/icons'
import { contentApi } from '@/utils/api/content'
import { showResetReadingModal } from './ResetReadingModal.vue'

const props = defineProps<{
    contentId: string
}>()

const router = useRouter()
const qContent = contentApi.useGet(() => props.contentId)
const qChildren = contentApi.useList(() => ({
    parent_id: props.contentId,
    sort: 'order',
    sort_order: 'asc',
}))

const readingStatus = computed(() => {
    const content = qContent.data.value
    if (!content) return null
    const children = qChildren.data.value?.data ?? []
    if (!qChildren.data.value) return null

    if (content.type.includes('series')) {
        const firstUnread = children.findIndex(child => {
            return child.user_data?.status !== 'completed'
        })
        if (firstUnread === -1) {
            return 'all-completed'
        } else if (firstUnread === 0 && children[firstUnread]!.user_data?.status != 'reading') {
            return 'starting'
        } else {
            return 'resume'
        }
    } else {
        // `0` is a real position for both a comic page and a book offset.
        const progress = content.user_data?.progress
        const started = typeof progress?.current_page === 'number' || isBookLocator(progress?.book)
        return started ? 'resume' : 'starting'
    }
})

const resume = computed(() => readingStatus.value === 'resume')

const ctrlModifier = useKeyModifier('Control')

async function onClick() {
    const content = qContent.data.value
    const rs = readingStatus.value
    if (rs == null || !content) return

    if (rs === 'all-completed') {
        const confirmed = await showResetReadingModal(props.contentId)
        if (confirmed) {
            const firstChild = qChildren.data.value?.data[0]
            if (firstChild) {
                router.push('/r/' + firstChild.id)
            }
        }
        return
    }

    let targetId = props.contentId
    if (content.type.includes('series')) {
        const firstUnread = qChildren.data.value!.data.find(child => {
            return child.user_data?.status !== 'completed'
        })
        if (!firstUnread) return
        targetId = firstUnread.id
    }

    if (ctrlModifier.value) {
        window.open(`/r/${targetId}?page=resume`, '_blank')
    } else {
        router.push({
            path: `/r/${targetId}`,
            query: {
                page: 'resume',
            },
        })
    }
}
</script>
