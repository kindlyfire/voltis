<template>
    <div class="flex flex-wrap gap-1">
        <AChip v-if="all || counts.added" size="sm" tone="success">
            <span aria-hidden="true">+{{ counts.added }}</span>
            <span class="sr-only">{{ counts.added }} added</span>
        </AChip>
        <AChip v-if="all || counts.updated" size="sm" tone="info">
            <span aria-hidden="true">~{{ counts.updated }}</span>
            <span class="sr-only">{{ counts.updated }} updated</span>
        </AChip>
        <AChip v-if="all || counts.removed" size="sm" tone="danger">
            <span aria-hidden="true">−{{ counts.removed }}</span>
            <span class="sr-only">{{ counts.removed }} removed</span>
        </AChip>
    </div>
</template>

<script setup lang="ts">
import type { ScanCounts } from '@/stores/scans'
import AChip from '@/ui/AChip.vue'

defineProps<{
    counts: ScanCounts
    /** Shows zero counts too. */
    all?: boolean
}>()
</script>
