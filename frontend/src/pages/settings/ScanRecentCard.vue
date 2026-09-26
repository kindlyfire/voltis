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
import type { ScanRecent } from '@/utils/api/types'
import { API_URL } from '@/utils/fetch'
import ScanCounts from './ScanCounts.vue'

const props = defineProps<{ entry: ScanRecent }>()

const src = computed(() => {
    if (!props.entry.has_cover) return null
    return `${API_URL}/files/cover/${props.entry.id}?v=${props.entry.file_mtime}`
})
</script>
