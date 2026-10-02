<template>
    <ADialog :open="open" :title="copy.title" size="sm" @update:open="v => !v && close(copy.keep)">
        <p v-if="kind === 'moved'" class="text-sm">
            Here: <strong>{{ here }}</strong> · Saved: <strong>{{ saved }}</strong>
        </p>
        <p v-else class="text-sm">{{ copy.message }}</p>
        <template #actions>
            <AButton variant="text" tone="neutral" @click="close(copy.other)">
                {{ copy.otherLabel }}
            </AButton>
            <AButton autofocus @click="close(copy.keep)">{{ copy.keepLabel }}</AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'

const props = defineProps<{
    open: boolean
    close: (choice: ConflictChoice) => void
    kind: 'moved' | 'completed' | 'reset'
    here: string
    saved: string
}>()

/** Dismissing keeps what this reader has. */
const COPY = {
    moved: {
        title: 'You read on another device',
        message: '',
        keep: 'stay',
        keepLabel: 'Stay here',
        other: 'go',
        otherLabel: 'Go there',
    },
    completed: {
        title: 'Completed on another device',
        message: 'This was marked completed elsewhere. Reading on keeps it completed.',
        keep: 'keep',
        keepLabel: 'Keep reading',
        other: 'reset',
        otherLabel: 'Reset & read again',
    },
    reset: {
        title: 'Cleared on another device',
        message: 'Its status and position were cleared elsewhere.',
        keep: 'continue',
        keepLabel: 'Continue here',
        other: 'start',
        otherLabel: 'Go to start',
    },
} as const
const copy = computed(() => COPY[props.kind])
</script>

<script lang="ts">
export type ConflictChoice = 'go' | 'stay' | 'keep' | 'reset' | 'continue' | 'start'
</script>
