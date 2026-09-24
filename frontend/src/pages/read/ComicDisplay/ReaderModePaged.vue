<template>
    <div class="reader-paged flex items-center justify-center">
        <template v-if="loader">
            <div v-if="loader.error" class="flex flex-col items-center gap-2">
                <div class="text-(--color-error)">{{ loader.error }}</div>
                <AButton variant="tonal" @click.stop="loader.load()">Retry</AButton>
            </div>
            <div
                v-else-if="loader.loading || !loader.blobUrl"
                class="flex items-center justify-center"
            >
                <ASpinner size="lg" />
            </div>
            <img
                v-else
                :src="loader.blobUrl"
                :alt="`Page ${reader.state!.page + 1}`"
                class="reader-paged__image"
            />
        </template>
    </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useLayoutStore } from '@/pages/_layout/useLayoutStore'
import AButton from '@/ui/AButton.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { useReaderStore } from './useComicDisplayStore'

const reader = useReaderStore()
const layout = useLayoutStore()
layout.navbarHidden.useLayer('comicReaderPaged', true)

const loader = computed(() => reader.state?.loaders[reader.state.page])
</script>

<style scoped>
.reader-paged {
    width: 100%;
    height: 100%;
    min-height: 100dvh;
}

.reader-paged__image {
    width: 100%;
    height: 100dvh;
    object-fit: contain;
}
</style>
