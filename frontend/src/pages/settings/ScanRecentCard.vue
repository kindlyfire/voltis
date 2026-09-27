<template>
    <div class="flex flex-col gap-1.5">
        <ACover :src="src" alt="">
            <template #bottomRight><ScanCounts :counts="entry" class="justify-end" /></template>
        </ACover>
        <p class="line-clamp-2 text-xs leading-snug font-medium" :title="entry.title">
            {{ entry.title }}
        </p>
    </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import ACover from '@/ui/ACover.vue'
import { coverUrl } from '@/utils/api/content'
import type { ScanRecent } from '@/utils/api/types'
import ScanCounts from './ScanCounts.vue'

const props = defineProps<{ entry: ScanRecent }>()

const src = computed(() => coverUrl(props.entry))
</script>
