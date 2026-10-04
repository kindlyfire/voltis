<template>
    <ADialog :open="open" :title="title" @update:open="v => !v && close()">
        <div class="mx-auto mb-3 w-full max-w-64">
            <!-- A frame border and separate interior dividers: an outline
            or per-zone borders collide with the clipped rounded corners. -->
            <div class="zone-frame relative aspect-3/4 overflow-hidden rounded-lg">
                <div
                    v-for="zone in zones"
                    :key="zone.inset"
                    class="absolute flex items-center justify-center"
                    :class="zone.tint"
                    :style="{ inset: zone.inset }"
                >
                    <AIcon :icon="zone.icon" class="text-xl" />
                </div>
                <div
                    v-for="(top, i) in [band, mid]"
                    :key="`h${i}`"
                    class="divider-h absolute right-0 left-0"
                    :style="{ top }"
                />
                <div
                    v-for="(left, i) in [firstCol, secondCol]"
                    :key="`v${i}`"
                    class="divider-v absolute"
                    :style="{ left, top: band, bottom: band }"
                />
            </div>
        </div>

        <div class="text-fg-muted mb-4 flex justify-center gap-4 text-xs">
            <span class="flex items-center gap-1"
                ><i class="prev inline-block size-2.5 rounded-[3px]" />Previous</span
            >
            <span class="flex items-center gap-1"
                ><i class="menu inline-block size-2.5 rounded-[3px]" />Menu</span
            >
            <span class="flex items-center gap-1"
                ><i class="next inline-block size-2.5 rounded-[3px]" />Next</span
            >
        </div>

        <p v-for="line in lines" :key="line" class="mb-2 text-sm last:mb-0">{{ line }}</p>

        <div v-if="!coarse" class="mt-4 text-sm">
            <ReaderHeading>Keyboard shortcuts</ReaderHeading>
            <div
                v-for="[keys, action] in shortcuts"
                :key="keys"
                class="flex justify-between gap-4 py-0.5 text-xs"
            >
                <span class="shrink-0">{{ action }}</span>
                <span class="text-fg-muted text-right font-mono">{{ keys }}</span>
            </div>
        </div>

        <template #actions>
            <AButton autofocus @click="close()">Got it</AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { useMediaQuery } from '@vueuse/core'
import { computed } from 'vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import AIcon from '@/ui/AIcon.vue'
import {
    IconChevronDown,
    IconChevronLeft,
    IconChevronRight,
    IconChevronUp,
    IconMenu,
} from '@/ui/icons'
import type { LayoutKind } from './BookDisplay/readingLayout'
import ReaderHeading from './ReaderHeading.vue'
import { bookShortcuts, comicShortcuts, type ReaderKind } from './shortcuts'
import { HEIGHT_ZONE } from './useClickZones'

const props = defineProps<{
    open: boolean
    close: () => void
    kind: ReaderKind
    bookMode?: LayoutKind
    /** Comic controls mirrored for right-to-left reading. */
    flipped?: boolean
}>()

const coarse = useMediaQuery('(pointer: coarse)')

const pct = (n: number) => `${Number(n.toFixed(3))}%`

// Inside a ~480px dialog the centre column is simply a third: the 300px cap in
// `getClickZone` never applies at this width.
const band = pct(HEIGHT_ZONE * 100)
const mid = pct(100 - HEIGHT_ZONE * 100)
const firstCol = pct(100 / 3)
const secondCol = pct(200 / 3)

// Flipped swaps only the side tints: the chevrons still point at the screen edges.
const zones = computed(() => {
    const [left, right] = props.flipped ? ['next', 'prev'] : ['prev', 'next']
    return [
        { icon: IconChevronUp, tint: 'prev', inset: `0 0 ${mid} 0` },
        { icon: IconChevronLeft, tint: left, inset: `${band} ${secondCol} ${band} 0` },
        { icon: IconMenu, tint: 'menu', inset: `${band} ${firstCol} ${band} ${firstCol}` },
        { icon: IconChevronRight, tint: right, inset: `${band} 0 ${band} ${secondCol}` },
        { icon: IconChevronDown, tint: 'next', inset: `${mid} 0 0 0` },
    ]
})

const title = computed(() =>
    props.kind === 'comic' ? 'Comic reader tutorial' : 'Book reader tutorial'
)
const shortcuts = computed(() =>
    props.kind === 'comic'
        ? comicShortcuts({ flipped: props.flipped ?? false, paged: true })
        : bookShortcuts(props.bookMode ?? 'paged')
)

const lines = computed(() => {
    const [action, gerund] = coarse.value ? ['Tap', 'tapping'] : ['Click', 'clicking']
    const menu = `The menu can be opened by ${gerund} the center of the screen.`
    const swipe = 'You can also swipe left or right.'
    if (props.kind === 'comic') {
        return [
            `${action} the previous and next zones to turn pages or scroll a page that doesn't fit. In longstrip mode they scroll the screen.`,
            ...(coarse.value ? [swipe] : []),
            menu,
        ]
    }
    if (props.bookMode === 'scroll') {
        return [
            `${action} the previous and next zones to scroll. That can also be used to go to the next chapter at the end.`,
            menu,
        ]
    }
    return [
        `${action} the previous and next zones to turn pages. Turning past the last page continues into the next chapter.`,
        coarse.value ? swipe : 'The mouse wheel also turns pages.',
        menu,
    ]
})
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './ReaderTutorialModal.vue'

export function showReaderTutorial(
    kind: ReaderKind,
    bookMode?: LayoutKind,
    flipped?: boolean
): Promise<void> {
    return Modals.show(Self, { kind, bookMode, flipped })
}
</script>

<style scoped>
.zone-frame {
    border: 1px solid var(--color-outline);
}
.divider-h,
.divider-v {
    border-color: var(--color-outline);
    border-style: dashed;
    border-width: 0;
}
.divider-h {
    border-top-width: 1px;
}
.divider-v {
    border-left-width: 1px;
}
.prev {
    background: color-mix(in oklch, var(--color-primary) 14%, transparent);
}
.next {
    background: color-mix(in oklch, var(--color-primary) 32%, transparent);
}
.menu {
    background: color-mix(in oklch, var(--color-fg-muted) 22%, transparent);
}
</style>
