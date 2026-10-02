<template>
    <AButton v-if="failed" :leading-icon="IconRefresh" tone="danger" @click="retry">
        Couldn't load · Retry
    </AButton>
    <AButton
        v-else-if="qContinue.data.value?.reason === 'empty'"
        :leading-icon="IconBookOpen"
        disabled
    >
        No readable {{ childNoun(type, 2) }}
    </AButton>
    <AButton
        v-else
        :leading-icon="IconBookOpen"
        :loading="!qContinue.data.value && qContinue.isFetching.value"
        @click="onClick"
    >
        {{ label }}
    </AButton>
</template>

<script setup lang="ts">
import { useKeyModifier } from '@vueuse/core'
import { computed, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import AButton from '@/ui/AButton.vue'
import { IconBookOpen, IconRefresh } from '@/ui/icons'
import { contentApi } from '@/utils/api/content'
import { readingApi } from '@/utils/api/reading'
import type { Content, ContentType } from '@/utils/api/types'
import { childNoun } from '@/utils/contentProgress'
import { itemName } from '@/utils/seriesItem'
import { showClearReadingModal } from './ClearReadingModal.vue'

const props = defineProps<{
    contentId: string
    type: ContentType
    /** Names the held item by its number in this series. */
    series?: Content | null
}>()

const router = useRouter()
const qContinue = readingApi.useContinue(() => props.contentId)
// Read again couldn't find the series' first volume; nothing was cleared.
const lookupFailed = ref(false)
watch(
    () => props.contentId,
    () => (lookupFailed.value = false)
)
let unmounted = false
onUnmounted(() => (unmounted = true))
const failed = computed(
    () =>
        lookupFailed.value ||
        (!qContinue.data.value && qContinue.isError.value && !qContinue.isFetching.value)
)

function retry() {
    if (lookupFailed.value) void readAgain()
    else void qContinue.refetch()
}

const ACTION_LABELS = { start: 'Start reading', resume: 'Continue reading', next: 'Read next' }

const label = computed(() => {
    const c = qContinue.data.value
    if (!c) return 'Continue reading'
    if (c.reason === 'held' && c.target) return `Resume ${itemName(c.target, props.series)}`
    if (c.reason === 'earlier_unread') return 'Read earlier volume'
    if (c.reason === 'completed' || c.reason === 'caught_up') return 'Read again'
    if (!c.action) return 'Start reading'
    return ACTION_LABELS[c.action]
})

const ctrlModifier = useKeyModifier('Control')

function open(id: string) {
    if (ctrlModifier.value) window.open(`/r/${id}?page=resume`, '_blank')
    else void router.push({ path: `/r/${id}`, query: { page: 'resume' } })
}

/** Read again starts over: the item itself, or a series' first volume, found before clearing. */
async function readAgain() {
    const id = props.contentId
    // The page may have moved on to other content meanwhile.
    const left = () => unmounted || props.contentId !== id
    lookupFailed.value = false
    let target = id
    if (qContinue.data.value?.series_id === id) {
        try {
            const first = await contentApi.ids(
                { parent_id: id, valid: true, sort: 'order', sort_order: 'asc' },
                0,
                1
            )
            if (!first.ids[0]) throw new Error('No readable volume')
            target = first.ids[0]
        } catch {
            if (!left()) lookupFailed.value = true
            return
        }
        if (left()) return
    }
    if ((await showClearReadingModal(id)) && !left()) open(target)
}

async function onClick() {
    const c = qContinue.data.value
    if (!c) return
    if (c.target) return open(c.target.id)
    if (c.reason === 'earlier_unread' && c.earlier_unread_id) {
        return void router.push(`/${c.earlier_unread_id}`)
    }
    if (c.reason === 'completed' || c.reason === 'caught_up') await readAgain()
}
</script>
