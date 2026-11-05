<template>
    <template v-if="row.links">
        <a
            v-for="link in shown(row.links)"
            :key="link.url"
            :href="link.url"
            target="_blank"
            rel="noopener"
            class="a-focus text-primary mr-3 inline-flex items-center gap-1 rounded-sm font-medium hover:underline"
        >
            {{ link.label }}
            <AIcon :icon="IconOpenInNew" />
            <span class="sr-only">(opens in a new tab)</span>
        </a>
    </template>
    <div v-else-if="row.items && chips" class="flex flex-wrap gap-1.5">
        <AChip v-for="(item, i) in row.items" :key="i" size="sm" :to="item.to">
            {{ item.text }}
        </AChip>
    </div>
    <!-- Truncated lists stay on one line. -->
    <span v-else-if="row.items" :class="{ 'min-w-0 truncate': limit }">
        <template v-for="(item, i) in shown(row.items)" :key="i">
            <template v-if="i">{{ ', ' }}</template>
            <RouterLink
                v-if="item.to"
                :to="item.to"
                class="a-focus text-primary rounded-sm font-medium hover:underline"
            >
                {{ item.text }}
            </RouterLink>
            <template v-else>{{ item.text }}</template>
            <template v-if="item.suffix">{{ item.suffix }}</template>
        </template>
    </span>
    <template v-else>{{ row.value }}</template>
    <button
        v-if="hidden > 0"
        type="button"
        class="a-focus text-primary ml-1 shrink-0 rounded-sm font-medium whitespace-nowrap hover:underline"
        @click="emit('more')"
    >
        and {{ hidden }} more
        <span class="sr-only">{{ row.label.toLowerCase() }}</span>
    </button>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import AChip from '@/ui/AChip.vue'
import AIcon from '@/ui/AIcon.vue'
import { IconOpenInNew } from '@/ui/icons'
import type { DetailRow } from './useContentDetails'

const props = defineProps<{
    row: DetailRow
    /** Show only this many items, then an "and N more" button. */
    limit?: number
}>()
const emit = defineEmits<{ more: [] }>()

const total = computed(() => (props.row.links ?? props.row.items)?.length ?? 0)
// Full lists of genres and tags read better as chips; truncated ones stay inline.
const chips = computed(() => !props.limit && ['genres', 'tags'].includes(props.row.key))
const hidden = computed(() => (props.limit ? Math.max(0, total.value - props.limit) : 0))
const shown = <T>(list: T[]) => (hidden.value ? list.slice(0, props.limit) : list)
</script>
