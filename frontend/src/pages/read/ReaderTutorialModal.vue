<template>
    <VDialog :model-value="open" @update:model-value="v => !v && close()" max-width="480">
        <VCard>
            <VCardTitle>{{ title }}</VCardTitle>
            <VCardText>
                <div class="mx-auto mb-3 w-full max-w-64">
                    <!-- A frame border and separate interior dividers: an outline
                    or per-zone borders collide with the clipped rounded corners. -->
                    <div class="zone-frame relative aspect-3/4 overflow-hidden rounded-lg">
                        <div
                            v-for="zone in zones"
                            :key="zone.icon"
                            class="absolute flex items-center justify-center"
                            :class="zone.tint"
                            :style="{ inset: zone.inset }"
                        >
                            <VIcon :icon="zone.icon" size="small" />
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

                <div class="mb-4 flex justify-center gap-4 text-xs opacity-75">
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

                <div v-if="!coarse" class="mt-4 text-sm opacity-60">
                    <div class="mb-1">Keyboard shortcuts</div>
                    <div
                        v-for="[keys, action] in shortcuts"
                        :key="keys"
                        class="flex justify-between gap-4 text-xs!"
                    >
                        <span class="shrink-0">{{ action }}</span>
                        <span class="text-right font-mono">{{ keys }}</span>
                    </div>
                </div>
            </VCardText>
            <VCardActions class="justify-end">
                <VBtn color="primary" variant="flat" @click="close()">Got it</VBtn>
            </VCardActions>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
import { useMediaQuery } from '@vueuse/core'
import { computed } from 'vue'
import { BOOK_SHORTCUTS, COMIC_SHORTCUTS, type ReaderKind } from './shortcuts'
import { HEIGHT_ZONE } from './useClickZones'

const props = defineProps<{
    open: boolean
    close: () => void
    kind: ReaderKind
}>()

const coarse = useMediaQuery('(pointer: coarse)')

const pct = (n: number) => `${Number(n.toFixed(3))}%`

// Inside a ~480px dialog the centre column is simply a third: the 300px cap in
// `getClickZone` never applies at this width.
const band = pct(HEIGHT_ZONE * 100)
const mid = pct(100 - HEIGHT_ZONE * 100)
const firstCol = pct(100 / 3)
const secondCol = pct(200 / 3)

const zones = [
    { icon: 'mdi-chevron-up', tint: 'prev', inset: `0 0 ${mid} 0` },
    { icon: 'mdi-chevron-left', tint: 'prev', inset: `${band} ${secondCol} ${band} 0` },
    { icon: 'mdi-menu', tint: 'menu', inset: `${band} ${firstCol} ${band} ${firstCol}` },
    { icon: 'mdi-chevron-right', tint: 'next', inset: `${band} 0 ${band} ${secondCol}` },
    { icon: 'mdi-chevron-down', tint: 'next', inset: `${mid} 0 0 0` },
]

const title = computed(() =>
    props.kind === 'comic' ? 'Comic reader tutorial' : 'Book reader tutorial'
)
const shortcuts = computed(() => (props.kind === 'comic' ? COMIC_SHORTCUTS : BOOK_SHORTCUTS))

const lines = computed(() => {
    const [action, gerund] = coarse.value ? ['Tap', 'tapping'] : ['Click', 'clicking']
    const menu = `The menu can be opened by ${gerund} the center of the screen.`
    if (props.kind === 'comic') {
        return [
            `${action} the previous and next zones to turn pages. In longstrip mode they scroll the screen instead.`,
            menu,
        ]
    }
    return [
        `${action} the previous and next zones to scroll. That can also be used to go to the next chapter at the end.`,
        menu,
    ]
})
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './ReaderTutorialModal.vue'

export function showReaderTutorial(kind: ReaderKind): Promise<void> {
    return Modals.show(Self, { kind })
}
</script>

<style scoped>
.zone-frame {
    border: 1px solid rgba(var(--v-theme-on-surface), 0.2);
}
.divider-h,
.divider-v {
    border-color: rgba(var(--v-theme-on-surface), 0.15);
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
    background: rgba(var(--v-theme-primary), 0.12);
}
.next {
    background: rgba(var(--v-theme-primary), 0.28);
}
.menu {
    background: rgba(var(--v-theme-secondary), 0.25);
}
</style>
