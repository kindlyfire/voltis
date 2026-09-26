<template>
    <TransitionGroup ref="strip" tag="ul" name="scan-strip" class="scan-strip" @before-leave="pin">
        <li key="lead">
            <ScanLeadCard :lead="row.lead" :label="label" />
        </li>
        <li v-for="(entry, i) in row.recent" :key="keys[i] ?? entry.id" :style="{ '--i': i }">
            <ScanRecentCard :entry="entry" />
        </li>
    </TransitionGroup>
</template>

<script setup lang="ts">
import { type ComponentPublicInstance, onBeforeUpdate, ref, useTemplateRef, watch } from 'vue'
import type { ScanRow } from '@/stores/scans'
import ScanLeadCard from './ScanLeadCard.vue'
import ScanRecentCard from './ScanRecentCard.vue'

const props = defineProps<{
    row: ScanRow
    /** Accessible name of the scan's progress bar. */
    label: string
}>()

let seq = 0
let prev: Keyed = { ids: [], keys: [] }
const keys = ref<string[]>([])

watch(
    () => props.row.recent.map(e => e.id),
    ids => {
        prev = { ids, keys: stripKeys(prev, ids, id => `${id}:${++seq}`) }
        keys.value = prev.keys
    },
    { immediate: true }
)

// A leaving card keeps its place while the others move. Offsets are measured before the patch:
// each leaving card turns absolute right after its hook, which shifts the ones after it.
const strip = useTemplateRef<ComponentPublicInstance>('strip')
const offsets = new WeakMap<Element, number>()

onBeforeUpdate(() => {
    for (const li of (strip.value?.$el as HTMLElement | undefined)?.children ?? []) {
        offsets.set(li, (li as HTMLElement).offsetLeft)
    }
})

function pin(el: Element): void {
    const li = el as HTMLElement
    li.style.left = `${offsets.get(li) ?? li.offsetLeft}px`
}
</script>

<script lang="ts">
export interface Keyed {
    ids: string[]
    keys: string[]
}

/**
 * Keys the next entries of the strip. An entry that moved towards the front gets a fresh key, so
 * its old card leaves in place and a new one enters at the front instead of sliding across.
 */
export function stripKeys(prev: Keyed, ids: string[], fresh: (id: string) => string): string[] {
    const at = new Map(prev.ids.map((id, i) => [id, i]))
    return ids.map((id, i) => {
        const was = at.get(id)
        return was !== undefined && i >= was ? prev.keys[was]! : fresh(id)
    })
}
</script>

<style scoped>
/* Exactly --cols cards fill the row; the rest sit past its right edge, clipped, so a card pushed
   out slides away. Each threshold is n cards of 116px plus n - 1 gaps of 12px. */
.scan-strip {
    --cols: 5;

    container-type: inline-size;
    position: relative;
    display: flex;
    gap: 12px;
    overflow: hidden;

    & > li {
        flex: none;
        width: calc((100cqw - (var(--cols) - 1) * 12px) / var(--cols));
    }
}

@container (width < 628px) {
    .scan-strip > li {
        --cols: 4;
    }
}

@container (width < 500px) {
    .scan-strip > li {
        --cols: 3;
    }
}

@container (width < 372px) {
    .scan-strip > li {
        --cols: 2;
    }
}

@container (width < 244px) {
    .scan-strip > li {
        --cols: 1;
    }
}

.scan-strip-enter-active,
.scan-strip-leave-active,
.scan-strip-move {
    transition:
        opacity var(--duration-medium) var(--ease-standard),
        transform var(--duration-medium) var(--ease-standard);
}

/* A batch of new cards cascades in from the front. */
.scan-strip-enter-active {
    transition-delay: calc(var(--i, 0) * var(--duration-short) / 3);
}

.scan-strip-leave-active {
    position: absolute;
    top: 0;
}

.scan-strip-enter-from {
    opacity: 0;
    transform: translateX(-24px);
}

.scan-strip-leave-to {
    opacity: 0;
    transform: translateX(24px);
}
</style>
