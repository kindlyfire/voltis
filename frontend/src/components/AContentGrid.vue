<template>
    <div>
        <div class="d-flex align-center justify-space-between mb-2">
            <span> {{ items.length }} items </span>
            <AContentGridSettings :store-key="storeKey" :width="width" />
        </div>

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
                    :store-key="storeKey"
                />
            </template>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, ref, toRef } from 'vue'
import { useElementSize } from '@vueuse/core'
import type { Content } from '@/utils/api/types'
import AContentGridItemSkeleton from './AContentGridItemSkeleton.vue'
import AContentGridItem from './AContentGridItem.vue'
import AContentGridSettings from './AContentGridSettings.vue'
import { useContentGridStore } from './useContentGridStore'

const props = withDefaults(
    defineProps<{
        items: Content[]
        loading: boolean
        toReadRoute?: boolean
        storeKey?: string
    }>(),
    { storeKey: 'default' }
)

const store = useContentGridStore()
const settings = store.getForKey(toRef(props, 'storeKey'))

const gridRef = ref<HTMLElement>()
const { width } = useElementSize(gridRef)

const cols = computed(() => {
    if (width.value <= 0) return 1
    return Math.max(1, Math.round(width.value / settings.value.itemSize))
})

const gridStyle = computed(() => ({
    gridTemplateColumns: `repeat(${cols.value}, 1fr)`,
}))
</script>
