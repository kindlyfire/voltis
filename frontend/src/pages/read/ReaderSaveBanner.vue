<template>
    <div v-if="message" class="reader-save-banner" role="status">
        <AAlert :tone="sync.stale ? 'info' : 'warning'">
            <div class="flex items-center gap-3">
                <span>{{ message }}</span>
                <AButton size="sm" variant="tonal" @click.stop="act">{{ actionLabel }}</AButton>
            </div>
        </AAlert>
    </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import AAlert from '@/ui/AAlert.vue'
import AButton from '@/ui/AButton.vue'
import type { ReadingSync } from './readingSync'

/** Saving trouble the reader should know about: repeated failures, or a conflict while away. */
const props = defineProps<{ sync: ReadingSync }>()

const message = computed(() =>
    props.sync.stale
        ? 'Changed on another device'
        : props.sync.failures >= 3
          ? 'Progress not saved'
          : null
)
const actionLabel = computed(() => (props.sync.stale ? 'Review' : 'Retry'))

function act() {
    if (props.sync.stale) props.sync.check()
    else props.sync.retry()
}
</script>

<style scoped>
.reader-save-banner {
    position: fixed;
    top: calc(var(--layout-top, 0px) + 8px);
    left: 50%;
    z-index: var(--z-reader-progress);
    transform: translateX(-50%);
    max-width: calc(100vw - 32px);
}
</style>
