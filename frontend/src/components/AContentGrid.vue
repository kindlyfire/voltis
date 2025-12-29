<template>
    <div :class="LIBRARY_GRID_CLASSES">
        <template v-if="loading">
            <AContentGridItemSkeleton />
            <AContentGridItemSkeleton />
            <AContentGridItemSkeleton />
        </template>

        <template v-else>
            <AContentGridItem v-for="item in items" :key="item.id" :content="item" />
        </template>
    </div>
</template>

<script setup lang="ts">
import type { Content } from '@/utils/api/types'
import { LIBRARY_GRID_CLASSES } from '@/utils/misc'
import AContentGridItemSkeleton from './AContentGridItemSkeleton.vue'
import AContentGridItem from './AContentGridItem.vue'

const props = defineProps<{
    items: Content[]
    loading: boolean
}>()
</script>
