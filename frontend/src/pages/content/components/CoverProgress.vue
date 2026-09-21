<template>
    <VProgressLinear
        v-if="progress"
        :model-value="progress.fraction * 100"
        height="18"
        color="primary"
        bg-color="black"
        bg-opacity="0.6"
    >
        <span class="text-xs font-medium text-white text-shadow-md/40">
            {{ progress.label }}
        </span>
    </VProgressLinear>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Content } from '@/utils/api/types'
import { contentProgress } from '@/utils/contentProgress'

const props = defineProps<{
    content: Content
}>()

const progress = computed(() => contentProgress(props.content))
</script>
