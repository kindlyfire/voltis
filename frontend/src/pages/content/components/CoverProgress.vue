<template>
    <div v-if="progress || newCount || earlierId" class="flex flex-col gap-1.5">
        <div
            v-if="progress || newCount"
            class="flex flex-wrap items-center justify-between gap-x-2 gap-y-1 text-[13px]"
        >
            <span v-if="progress" aria-hidden="true">Progress</span>
            <span class="flex items-center gap-1.5">
                <AChip v-if="newCount" size="sm" tone="primary">{{ newCount }} new</AChip>
                <span v-if="progress" class="text-fg-muted" aria-hidden="true">
                    {{ progress.label }}
                </span>
            </span>
        </div>
        <AProgressBar
            v-if="progress"
            :value="progress.fraction"
            label="Reading progress"
            :value-text="progress.label"
        />
        <RouterLink
            v-if="earlierId"
            :to="`/${earlierId}`"
            class="a-focus text-primary self-start rounded-sm text-[13px] font-medium hover:underline"
        >
            Earlier unread {{ childNoun(content.type, 2) }}
        </RouterLink>
    </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import AChip from '@/ui/AChip.vue'
import AProgressBar from '@/ui/AProgressBar.vue'
import { readingApi } from '@/utils/api/reading'
import type { Content } from '@/utils/api/types'
import {
    childNoun,
    contentProgress,
    seriesPosition,
    seriesReadLabel,
    type ContentProgress,
} from '@/utils/contentProgress'

const props = defineProps<{
    content: Content
}>()

const isSeries = computed(() => props.content.type.endsWith('_series'))
const status = computed(() => props.content.user_data?.status ?? null)

// Unlike a grid card's bar, a series' bar stays at 100% and whatever its status: it carries the
// series' read count.
const progress = computed((): ContentProgress | null => {
    if (!isSeries.value) return contentProgress(props.content)
    const position = seriesPosition(props.content)
    if (!position || position.read <= 0) return null
    let label = seriesReadLabel(props.content)!
    if (position.read >= position.total && status.value !== 'completed') label += ' · Caught up'
    return { fraction: position.read / position.total, label }
})

const newCount = computed(() =>
    status.value === 'completed' ? (props.content.new_children_count ?? 0) : 0
)

const qContinue = readingApi.useContinue(() => (isSeries.value ? props.content.id : null))
const earlierId = computed(() => qContinue.data.value?.earlier_unread_id ?? null)
</script>
