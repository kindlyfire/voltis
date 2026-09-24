<template>
    <div
        ref="containerRef"
        id="longstrip-container"
        class="reader-longstrip flex flex-col items-center"
    >
        <template v-for="(loader, index) in reader.state?.loaders ?? []" :key="index">
            <div
                class="reader-longstrip__page"
                :id="`longstrip-page-${index}`"
                :style="getPageStyle(index)"
            >
                <div
                    v-if="loader.error"
                    class="reader-longstrip__placeholder flex flex-col items-center justify-center gap-2"
                >
                    <div class="text-(--color-error)">{{ loader.error }}</div>
                    <AButton variant="tonal" size="sm" @click.stop="loader.load()">Retry</AButton>
                </div>
                <div
                    v-else-if="loader.loading || !loader.blobUrl"
                    class="reader-longstrip__placeholder flex items-center justify-center"
                >
                    <ASpinner decorative />
                </div>
                <img
                    v-else
                    :src="loader.blobUrl"
                    :alt="`Page ${index + 1}`"
                    class="reader-longstrip__image"
                />
            </div>
        </template>
    </div>

    <div class="browser-ui-padding"></div>
</template>

<script setup lang="ts">
import { useDebounceFn, useEventListener } from '@vueuse/core'
import { ref, onMounted } from 'vue'
import { useNavbarScrollHide } from '@/pages/_layout/useLayoutStore'
import AButton from '@/ui/AButton.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { useReaderStore } from './useComicDisplayStore'

const reader = useReaderStore()
const containerRef = ref<HTMLElement | null>(null)
useNavbarScrollHide()

function getPageStyle(index: number) {
    const page = reader.state?.pageDimensions[index]
    if (!page) return {}
    const widthPercent = reader.settings.longstripWidth
    return {
        width: `min(${widthPercent}%, ${page.width}px)`,
        aspectRatio: `${page.width} / ${page.height}`,
    }
}

// Update current page based on scroll position
const updateCurrentPage = useDebounceFn(
    () => {
        if (!containerRef.value) return

        const container = containerRef.value
        const children = Array.from(container.children) as HTMLElement[]
        const viewportCenter = window.scrollY + window.innerHeight / 2

        // Find page at center of viewport
        for (let i = children.length - 1; i >= 0; i--) {
            const el = children[i]!
            if (el.offsetTop <= viewportCenter) {
                reader.setPage(i)
                break
            }
        }
    },
    50,
    { maxWait: 150 }
)

useEventListener(window, 'scroll', updateCurrentPage)

onMounted(() => {
    reader.goToPage()
})
</script>

<style scoped>
.reader-longstrip {
    width: 100%;
    min-height: calc(100dvh - var(--layout-top, 0px));
}

.reader-longstrip__page {
    max-width: 100%;
}

.reader-longstrip__placeholder {
    width: 100%;
    height: 100%;
    min-height: 200px;
    background: rgba(128, 128, 128, 0.1);
}

.reader-longstrip__image {
    width: 100%;
    height: auto;
    display: block;
}

.browser-ui-padding {
    height: calc(100lvh - 100svh);
}
</style>
