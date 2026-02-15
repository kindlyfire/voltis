<template>
    <div ref="gridRef" class="grid gap-4" :style="gridStyle">
        <template v-if="loading">
            <AContentGridItemSkeleton />
            <AContentGridItemSkeleton />
            <AContentGridItemSkeleton />
        </template>

        <template v-else>
            <AContentGridItem
                v-for="item in items"
                :key="item.id"
                :content="item"
                :to-read-route="toReadRoute"
            />
        </template>
    </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useElementSize } from '@vueuse/core'
import type { Content } from '@/utils/api/types'
import AContentGridItemSkeleton from './AContentGridItemSkeleton.vue'
import AContentGridItem from './AContentGridItem.vue'

export interface AContentGridSettings {
    itemSize?: number
}

const DEFAULT_ITEM_SIZE = 170

const props = defineProps<{
    items: Content[]
    loading: boolean
    toReadRoute?: boolean
    settings?: AContentGridSettings
}>()

const gridRef = ref<HTMLElement>()
const { width } = useElementSize(gridRef)

const cols = computed(() => {
    const target = props.settings?.itemSize ?? DEFAULT_ITEM_SIZE
    if (width.value <= 0) return 1
    return Math.max(1, Math.round(width.value / target))
})

const gridStyle = computed(() => ({
    gridTemplateColumns: `repeat(${cols.value}, 1fr)`,
}))
</script>
