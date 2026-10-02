<template>
    <div class="flex flex-col items-center gap-3 text-center">
        <template v-if="series">
            <p class="text-fg-muted text-sm">
                <template v-if="series.caught_up">Caught up · </template
                >{{ seriesReadLabel(series) }}
            </p>
            <RouterLink
                v-if="earlierId"
                :to="`/${earlierId}`"
                class="text-primary text-sm underline"
                @click.stop
            >
                Read earlier volume
            </RouterLink>
            <AButton
                v-if="series.status !== 'completed'"
                :variant="series.caught_up ? 'filled' : 'tonal'"
                @click.stop="sync.completeSeries()"
            >
                Mark series completed
            </AButton>
        </template>
        <p v-else-if="status" class="text-fg-muted text-sm">{{ READING_STATUS_LABELS[status] }}</p>
        <AButton variant="tonal" :to="exit.to" @click.stop="emit('leave')">{{
            exit.label
        }}</AButton>
    </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import AButton from '@/ui/AButton.vue'
import { readingApi } from '@/utils/api/reading'
import { READING_STATUS_LABELS } from '@/utils/api/types'
import { seriesReadLabel } from '@/utils/contentProgress'
import type { ReaderExit } from './readerExit'
import type { ReadingSync } from './readingSync'

/** The end of the last volume (or of a standalone item): where the series stands, and the exit. */
const props = defineProps<{ sync: ReadingSync; exit: ReaderExit }>()
const emit = defineEmits<{ leave: [] }>()

const series = computed(() => props.sync.series)
const status = computed(() => props.sync.acked?.status ?? null)
const qContinue = readingApi.useContinue(() => series.value?.id)
const earlierId = computed(() => qContinue.data.value?.earlier_unread_id ?? null)
</script>
