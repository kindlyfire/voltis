<template>
    <div v-if="progress" class="flex flex-col gap-1.5">
        <div class="flex justify-between gap-2 text-[13px]" aria-hidden="true">
            <span>Progress</span>
            <span class="text-fg-muted">{{ progress.label }}</span>
        </div>
        <AProgressBar
            :value="progress.fraction"
            label="Reading progress"
            :value-text="progress.label"
        />
    </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import AProgressBar from '@/ui/AProgressBar.vue'
import type { Content } from '@/utils/api/types'
import { contentProgress } from '@/utils/contentProgress'

const props = defineProps<{
    content: Content
}>()

const progress = computed(() => contentProgress(props.content))
</script>
