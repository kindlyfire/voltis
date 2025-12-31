<template>
    <VBtn
        size="large"
        class="h-12!"
        :class="class"
        variant="tonal"
        :disabled="readingStatus == null"
        @click="onClick"
    >
        <template
            v-if="
                readingStatus == null ||
                readingStatus === 'all-completed' ||
                readingStatus == 'starting'
            "
        >
            Start Reading
        </template>
        <template v-else>Continue Reading</template>
    </VBtn>
    <ResetReadingModal v-model="showResetModal" :content-id="props.contentId" />
</template>

<script setup lang="ts">
import { contentApi } from '@/utils/api/content'
import { useKeyModifier } from '@vueuse/core'
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import ResetReadingModal from './ResetReadingModal.vue'

const props = defineProps<{
    contentId: string
    class?: string
}>()

const showResetModal = ref(false)

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
        if (content.user_data?.progress?.current_page) {
            return 'resume'
        } else {
            return 'starting'
        }
    }
})

const ctrlModifier = useKeyModifier('Control')

function onClick() {
    const content = qContent.data.value
    const rs = readingStatus.value
    if (rs == null || !content) return

    if (rs === 'all-completed') {
        showResetModal.value = true
        return
    }

    let targetId = props.contentId
    if (content.type.includes('series')) {
        const firstUnread = qChildren.data.value!.data.find(child => {
            return child.user_data?.status !== 'completed'
        })
        if (!firstUnread) return
    }

    const userData = qContent.data.value?.user_data
    if (!userData?.status) {
        contentApi.updateUserData(props.contentId, {
            status: 'reading',
        })
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
