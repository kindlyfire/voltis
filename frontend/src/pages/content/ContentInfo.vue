<template>
    <dl class="grid max-w-[55rem] gap-x-8 gap-y-5 text-sm sm:grid-cols-[8rem_1fr]">
        <template v-for="row in details" :key="row.key">
            <dt class="font-medium sm:pt-0.5">{{ row.label }}</dt>
            <dd class="text-fg-muted -mt-3 min-w-0 [overflow-wrap:anywhere] sm:mt-0">
                <DetailValue :row="row" />
            </dd>
        </template>
    </dl>
</template>

<script setup lang="ts">
import DetailValue from './DetailValue.vue'
import type { DetailRow } from './useContentDetails'

defineProps<{ details: DetailRow[] }>()
</script>
