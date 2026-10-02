<template>
    <div v-if="siblings.status === 'error'" class="flex flex-col items-center gap-2 text-sm">
        <span class="text-fg-muted">Couldn't find out what comes next.</span>
        <AButton size="sm" variant="tonal" @click.stop="siblings.retry()">Retry</AButton>
    </div>
</template>

<script setup lang="ts">
import AButton from '@/ui/AButton.vue'
import type { Siblings } from './useSiblings'

/** The end of a volume whose series' other volumes failed to load. */
defineProps<{ siblings: Siblings }>()
</script>
