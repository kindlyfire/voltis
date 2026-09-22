<template>
    <VNavigationDrawer
        v-model="reader.sidebarOpen"
        temporary
        disable-route-watcher
        location="right"
        width="350"
        :style="{
            top: '0',
            height: '100vh',
        }"
    >
        <div class="mt-16 space-y-4! p-4 pt-0" v-if="reader.state">
            <!-- mb-2!: The spacing of 4 with the element below feels kinda wrong,
            because of how much empty space there is in this element. -->
            <div class="mb-2! flex h-16 items-center">
                <span class="text-h6">Reader</span>
                <VSpacer />
                <VBtn icon variant="text" @click="reader.sidebarOpen = false">
                    <VIcon>mdi-close</VIcon>
                </VBtn>
            </div>

            <div class="flex items-center justify-center">
                <VSkeletonLoader v-if="!parent" width="80%" height="1.5rem" />
                <template v-else>
                    <RouterLink
                        :to="`/${parent.id}`"
                        class="font-medium text-blue-400 hover:underline"
                    >
                        {{ parent.title }}
                    </RouterLink>
                </template>
            </div>

            <div v-if="reader.siblings">
                <div class="mb-2 flex items-center gap-2">
                    <VBtn
                        icon
                        size="small"
                        variant="tonal"
                        :disabled="reader.siblings.currentIndex === 0"
                        @click="reader.goToSibling('prev', true)"
                    >
                        <VIcon>mdi-chevron-left</VIcon>
                    </VBtn>
                    <VSelect
                        :model-value="reader.siblings.items[reader.siblings.currentIndex]?.id"
                        :items="reader.siblings.items"
                        item-title="title"
                        item-value="id"
                        density="compact"
                        hide-details
                        class="grow"
                        @update:model-value="reader.goToSibling($event)"
                        :loading="reader.state.loading || reader.qSiblings.isLoading"
                    />
                    <VBtn
                        icon
                        size="small"
                        variant="tonal"
                        :disabled="reader.siblings.currentIndex >= reader.siblings.items.length - 1"
                        @click="reader.goToSibling('next')"
                    >
                        <VIcon>mdi-chevron-right</VIcon>
                    </VBtn>
                </div>
                <div class="text-center text-sm opacity-60">
                    {{ reader.siblings.currentIndex + 1 }} of
                    {{ reader.siblings.items.length }}
                </div>
            </div>

            <div>
                <div class="mb-1 text-sm opacity-60">
                    Page {{ sliderPage + 1 }} of
                    {{ reader.state.pageDimensions.length }}
                </div>
                <VSlider
                    :model-value="sliderPage"
                    :min="0"
                    :max="Math.max(0, reader.state.pageDimensions.length - 1)"
                    :step="1"
                    hide-details
                    @update:model-value="onSliderChange"
                />
            </div>

            <div>
                <div class="mb-2 text-sm opacity-60">Mode</div>
                <VBtnToggle
                    :model-value="(reader.seriesSettings.mode ?? 'null') as ReaderMode | 'null'"
                    @update:model-value="reader.setMode($event == 'null' ? null : $event)"
                    mandatory
                    variant="outlined"
                    divided
                    class="w-full"
                >
                    <VBtn value="paged" class="flex-1">Paged</VBtn>
                    <VBtn value="longstrip" class="flex-1">Longstrip</VBtn>
                    <VBtn value="null" class="flex-1">Auto</VBtn>
                </VBtnToggle>
                <template v-if="reader.seriesSettings.mode == null">
                    <div class="mt-1 text-xs opacity-60">
                        Auto: {{ reader.mode === 'longstrip' ? 'Longstrip' : 'Paged' }}
                    </div>
                </template>
            </div>

            <div v-if="reader.mode === 'longstrip'">
                <div class="mb-1 text-sm opacity-60">
                    Width: {{ reader.settings.longstripWidth }}%
                </div>
                <VSlider
                    :model-value="reader.settings.longstripWidth"
                    @update:model-value="setLongstripWidth"
                    :min="10"
                    :max="100"
                    :step="5"
                    hide-details
                />
            </div>

            <div class="text-sm opacity-60">
                <div class="mb-1">Keyboard shortcuts</div>
                <div
                    v-for="[keys, action] in kbShortcuts"
                    :key="keys"
                    class="flex justify-between text-xs!"
                >
                    <span>{{ action }}</span>
                    <span class="font-mono">{{ keys }}</span>
                </div>
            </div>
        </div>
    </VNavigationDrawer>
</template>

<script setup lang="ts">
import { useDebounceFn } from '@vueuse/core'
import { onUnmounted, ref, watch, type Ref } from 'vue'
import { useLayoutStore } from '@/pages/_layout/useLayoutStore'
import { contentApi } from '@/utils/api/content'
import type { Content } from '@/utils/api/types'
import { COMIC_SHORTCUTS as kbShortcuts } from '../shortcuts'
import type { ReaderMode } from './types'
import { useReaderStore } from './useComicDisplayStore'

const reader = useReaderStore()
const layout = useLayoutStore()

const navbarHidden = layout.navbarHidden.useLayer('comicReaderSidebar')
watch(
    () => reader.sidebarOpen,
    open => {
        navbarHidden(open ? false : undefined)
    }
)

/** Page slider handling with debounce for actual updates */
const sliderPage = ref(reader.state?.page ?? 0)
watch(
    () => reader.state?.page,
    page => {
        if (page !== undefined) sliderPage.value = page
    }
)
const debouncedGoToPage = useDebounceFn((page: number) => {
    reader.goToPage(page)
}, 200)
function onSliderChange(page: number) {
    sliderPage.value = page
    debouncedGoToPage(page)
}

/** Parent content. Since this involves another fetch, when the content changes,
 * we keep the old parent around while the new one is loaded. */
const parent = ref(null) as Ref<Content | null>
watch(
    () => reader.state?.content,
    async content => {
        if (!content) return
        if (content.parent_id) {
            parent.value = await contentApi.get(content.parent_id)
        } else {
            parent.value = null
        }
    },
    {
        immediate: true,
    }
)

// Changing the width will change the scroll position, which means it changes
// the page. We do this keep the position stable.
let originalPage = null as number | null
function setLongstripWidth(width: number) {
    if (originalPage === null) {
        originalPage = reader.state?.page ?? null
    }
    reader.settings.longstripWidth = width
    requestAnimationFrame(() => {
        if (originalPage !== null) {
            reader.goToPage(originalPage, 'instant')
            originalPage = null
        }
    })
}

onUnmounted(() => {
    originalPage = null
})
</script>
