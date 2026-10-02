<template>
    <div class="flex items-center gap-2">
        <AChip size="sm" :tone="chip.tone" class="min-w-0 truncate">{{ chip.label }}</AChip>
        <AButton
            size="sm"
            variant="tonal"
            class="ml-auto shrink-0"
            :loading="busy"
            @click="run(primary.action)"
        >
            {{ primary.label }}
        </AButton>
        <AMenu align="end">
            <template #trigger>
                <AIconButton
                    :icon="IconDotsVertical"
                    label="More reading actions"
                    variant="standard"
                    size="sm"
                />
            </template>
            <AMenuItem v-for="item in overflow" :key="item.label" @select="run(item.action)">
                {{ item.label }}
            </AMenuItem>
        </AMenu>
    </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import AButton from '@/ui/AButton.vue'
import AChip from '@/ui/AChip.vue'
import AIconButton from '@/ui/AIconButton.vue'
import AMenu from '@/ui/AMenu.vue'
import AMenuItem from '@/ui/AMenuItem.vue'
import { IconDotsVertical } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { READING_STATUS_LABELS } from '@/utils/api/types'
import { RequestError } from '@/utils/fetch'
import type { ReadingSync } from './readingSync'

const props = defineProps<{ sync: ReadingSync }>()

interface Action {
    label: string
    action: () => Promise<unknown> | void
}

const toast = useToast()
const busy = ref(false)
const status = computed(() => props.sync.acked?.status ?? null)
const series = computed(() => props.sync.series)
const seriesHeld = computed(
    () => series.value?.status === 'on_hold' || series.value?.status === 'dropped'
)

const actions = {
    complete: {
        label: 'Mark completed',
        action: () => props.sync.command({ op: 'mark_completed' }),
    },
    resumeSeries: { label: 'Resume series', action: () => props.sync.seriesCommand('reading') },
    reading: {
        label: 'Set to Reading',
        action: () => props.sync.command({ op: 'set_status', status: 'reading' }),
    },
    reset: { label: 'Reset & read again', action: () => props.sync.resetAndReadAgain() },
    track: { label: 'Track progress', action: () => props.sync.trackProgress() },
} satisfies Record<string, Action>

const chip = computed(() => {
    const label = status.value ? READING_STATUS_LABELS[status.value] : 'No status'
    if (!props.sync.tracking) return { label: `${label} · not tracking`, tone: 'neutral' as const }
    if (status.value === 'completed') return { label, tone: 'success' as const }
    if (seriesHeld.value) {
        return {
            label: `Series is ${READING_STATUS_LABELS[series.value!.status!]}`,
            tone: 'warning' as const,
        }
    }
    if (series.value?.caught_up && series.value.status === 'reading') {
        return { label: `${label} · Caught up`, tone: 'primary' as const }
    }
    return { label, tone: status.value === 'reading' ? ('primary' as const) : ('neutral' as const) }
})

const primary = computed<Action>(() => {
    if (!props.sync.tracking) return actions.track
    if (status.value === 'completed') return actions.reset
    if (seriesHeld.value) return actions.resumeSeries
    return actions.complete
})

const overflow = computed(() => {
    const items: Action[] = []
    if (status.value !== 'completed') items.push(actions.complete)
    if (seriesHeld.value) items.push(actions.resumeSeries)
    if (status.value !== 'reading') items.push(actions.reading)
    items.push(actions.reset)
    return items.filter(a => a !== primary.value)
})

async function run(action: Action['action']) {
    busy.value = true
    try {
        await action()
    } catch (err) {
        toast.show({ message: `Couldn't update: ${RequestError.getMessage(err)}`, tone: 'danger' })
    } finally {
        busy.value = false
    }
}
</script>
