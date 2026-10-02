<template>
    <ADialog :open="open" :title="title" size="sm" @update:open="v => !v && close('keep')">
        <p class="text-sm">
            <template v-if="item">{{ item }} </template>This series is {{ label }}. Move it to
            Reading?
        </p>
        <template #actions>
            <AButton
                v-if="item"
                variant="text"
                tone="neutral"
                class="mr-auto"
                @click="close('undo')"
            >
                Undo
            </AButton>
            <AButton variant="text" tone="neutral" @click="close('keep')">Keep {{ label }}</AButton>
            <AButton autofocus @click="close('move')">Move to Reading</AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import { READING_STATUS_LABELS } from '@/utils/api/types'

const props = defineProps<{
    open: boolean
    close: (choice: SeriesHeldChoice) => void
    status: 'on_hold' | 'dropped'
    /** What reading just did to the volume, when it can be undone. */
    item: string | null
}>()

const label = computed(() => READING_STATUS_LABELS[props.status])
const title = computed(() => `Series ${label.value.toLowerCase()}`)
</script>

<script lang="ts">
export type SeriesHeldChoice = 'move' | 'keep' | 'undo'
</script>
