<template>
    <div ref="root" class="book-end flex flex-col items-center justify-center gap-4 p-4">
        <h2 ref="heading" tabindex="-1" class="font-display text-2xl font-semibold outline-none">
            End of book
        </h2>
        <template v-if="next">
            <p :id="nextId" class="max-w-md text-center text-balance">{{ next.title }}</p>
            <AButton
                class="book-end__next"
                :aria-describedby="nextId"
                @click.stop="emit('open', next.id)"
            >
                Read next
            </AButton>
        </template>
        <!-- Surely the last volume, or a standalone book: where the series stands. -->
        <ReaderEndSummary
            v-if="!next && siblings.status === 'ready' && sync"
            :sync="sync"
            :exit="exit"
            @leave="emit('leave')"
        />
        <template v-else>
            <SiblingsRetry v-if="!next" :siblings="siblings" />
            <AButton variant="tonal" :to="exit.to" @click.stop="emit('leave')">
                {{ exit.label }}
            </AButton>
        </template>
    </div>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import AButton from '@/ui/AButton.vue'
import type { Content } from '@/utils/api/types'
import ReaderEndSummary from '../ReaderEndSummary.vue'
import type { ReaderExit } from '../readerExit'
import type { ReadingSync } from '../readingSync'
import SiblingsRetry from '../SiblingsRetry.vue'
import type { Siblings } from '../useSiblings'

const props = defineProps<{
    exit: ReaderExit
    next: Content | null
    siblings: Siblings
    sync?: ReadingSync
}>()
const emit = defineEmits<{
    open: [id: string]
    leave: []
    /** Unmounting with focus inside, so it has to be handed on. */
    blur: []
}>()

const nextId = useId()
const root = ref<HTMLElement>()
const heading = ref<HTMLElement>()

function focusNext() {
    root.value?.querySelector<HTMLElement>('.book-end__next')?.focus()
}

onMounted(() => {
    if (props.next) focusNext()
    else heading.value?.focus()
})

// The sibling list can arrive after the screen opens.
watch(
    () => props.next,
    async next => {
        if (!next || document.activeElement !== heading.value) return
        await nextTick()
        focusNext()
    }
)

onBeforeUnmount(() => {
    if (root.value?.contains(document.activeElement)) emit('blur')
})
</script>

<style scoped>
.book-end {
    position: absolute;
    inset: 0;
    z-index: 1;
    background: var(--color-bg);
}
</style>
