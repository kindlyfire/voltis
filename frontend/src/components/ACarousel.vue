<template>
    <section class="carousel-section">
        <div class="section-header">
            <h2 class="text-h5 mb-0">{{ title }}</h2>
            <div class="controls">
                <VBtn
                    icon="mdi-chevron-left"
                    variant="text"
                    size="small"
                    @click="scroll(-1)"
                    :aria-label="`Scroll ${title} left`"
                />
                <VBtn
                    icon="mdi-chevron-right"
                    variant="text"
                    size="small"
                    @click="scroll(1)"
                    :aria-label="`Scroll ${title} right`"
                />
            </div>
        </div>
        <div class="carousel" role="list" ref="carouselRef">
            <slot />
        </div>
    </section>
</template>

<script setup lang="ts">
import { ref } from 'vue'

defineProps<{
    title: string
}>()

const carouselRef = ref<HTMLElement | null>(null)

function scroll(dir: 1 | -1) {
    const node = carouselRef.value
    if (!node) return
    const minScrollAmount = node.firstElementChild?.clientWidth ?? 0
    const delta = Math.max(node.clientWidth * 0.9, minScrollAmount) * dir
    node.scrollBy({ left: delta, behavior: 'smooth' })
}
</script>

<style scoped>
.carousel-section {
    display: flex;
    flex-direction: column;
    gap: 12px;
}

.section-header {
    display: flex;
    align-items: center;
    gap: 12px;
}

.controls {
    display: flex;
    gap: 4px;
}

.carousel {
    display: grid;
    grid-auto-flow: column;
    grid-auto-columns: 120px;
    gap: 16px;
    overflow-x: auto;
    padding-bottom: 8px;
    scroll-snap-type: x mandatory;
    scroll-padding: 8px;
}

.carousel::-webkit-scrollbar {
    display: none;
}

@media (min-width: 640px) {
    .carousel {
        grid-auto-columns: 150px;
    }
}

@media (min-width: 960px) {
    .carousel {
        grid-auto-columns: 180px;
    }
}
</style>
