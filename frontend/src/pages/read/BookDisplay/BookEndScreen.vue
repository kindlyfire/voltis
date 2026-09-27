<template>
    <div ref="root" class="book-end flex flex-col items-center justify-center gap-4 p-4">
        <h2 ref="heading" tabindex="-1" class="font-display text-2xl font-semibold outline-none">
            End of book
        </h2>
        <AButton v-if="next" class="book-end__next" @click.stop="emit('open', next.id)">
            Next: {{ next.title }}
        </AButton>
        <AButton variant="tonal" :to="`/${contentId}`" @click.stop="emit('leave')">
            Back to the book
        </AButton>
    </div>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import AButton from '@/ui/AButton.vue'
import type { Content } from '@/utils/api/types'

const props = defineProps<{ contentId: string; next: Content | null }>()
const emit = defineEmits<{
    open: [id: string]
    leave: []
    /** Unmounting with focus inside, so it has to be handed on. */
    blur: []
}>()

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
