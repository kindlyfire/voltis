<template>
    <ADialog :open="open" :title="copy.title" size="sm" @update:open="v => !v && close('stay')">
        <p v-if="kind === 'moved'" class="text-sm">
            Here: <strong>{{ here }}</strong> · Saved: <strong>{{ saved }}</strong>
        </p>
        <p v-else-if="message" class="text-sm">{{ message }}</p>
        <template #actions>
            <AButton variant="text" tone="neutral" @click="close('stay')">Stay here</AButton>
            <AButton autofocus @click="close('follow')">{{ followLabel }}</AButton>
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
    kind: ConflictKind
    here: string
    saved: string
    /** Where following leaves for: finished and series only. */
    next: { label: string; title: string | null } | null
}>()

/** Dismissing stays. */
const COPY = {
    moved: { title: 'You read on another device', message: '', follow: 'Move to new position' },
    cleared: {
        title: 'Cleared on another device',
        message: 'Its status and position were cleared.',
        follow: 'Go to start',
    },
    finished: { title: 'Finished on another device', message: '', follow: '' },
    series: {
        title: 'Series completed',
        message: 'This series was completed on another device.',
        follow: '',
    },
} as const
const copy = computed(() => COPY[props.kind])
const message = computed(() =>
    props.kind === 'finished'
        ? props.next?.title
            ? `Continue with ${props.next.title}?`
            : ''
        : copy.value.message
)
const followLabel = computed(() => props.next?.label ?? copy.value.follow)
</script>

<script lang="ts">
export type ConflictKind = 'moved' | 'cleared' | 'finished' | 'series'
export type ConflictChoice = 'stay' | 'follow'
</script>
