<template>
    <ADrawer v-slot="{ titleId }" v-model:open="reader.sidebarOpen" side="right" :width="350">
        <div class="space-y-4 p-4 pt-0" v-if="reader.state">
            <!-- mb-2: The spacing of 4 with the element below feels kinda wrong,
            because of how much empty space there is in this element. -->
            <div class="mb-2 flex h-16 items-center justify-between">
                <h2 :id="titleId" class="font-display text-xl font-semibold">Reader</h2>
                <AIconButton :icon="IconClose" label="Close" @click="reader.sidebarOpen = false" />
            </div>

            <div class="flex items-center justify-center">
                <ASkeleton v-if="!parent" width="80%" height="1.5rem" />
                <template v-else>
                    <RouterLink
                        :to="`/${parent.id}`"
                        class="text-primary font-medium hover:underline"
                    >
                        {{ parent.title }}
                    </RouterLink>
                </template>
            </div>

            <div v-if="reader.siblings">
                <div class="mb-2 flex items-center gap-2">
                    <AIconButton
                        :icon="IconChevronLeft"
                        label="Previous chapter"
                        variant="tonal"
                        size="sm"
                        :disabled="reader.siblings.currentIndex === 0"
                        @click="reader.goToSibling('prev', true)"
                    />
                    <ACombobox
                        :model-value="reader.siblings.items[reader.siblings.currentIndex]?.id"
                        :options="chapterOptions"
                        label="Chapter"
                        size="sm"
                        class="grow"
                        :loading="reader.state.loading || reader.qSiblings.isLoading"
                        @update:model-value="id => id && reader.goToSibling(id)"
                    />
                    <AIconButton
                        :icon="IconChevronRight"
                        label="Next chapter"
                        variant="tonal"
                        size="sm"
                        :disabled="reader.siblings.currentIndex >= reader.siblings.items.length - 1"
                        @click="reader.goToSibling('next')"
                    />
                </div>
                <div class="text-fg-muted text-center text-sm">
                    {{ reader.siblings.currentIndex + 1 }} of
                    {{ reader.siblings.items.length }}
                </div>
            </div>

            <ASlider
                :model-value="sliderPage"
                label="Page"
                show-value
                :format-value="page => `${page + 1} of ${reader.state!.pageDimensions.length}`"
                :min="0"
                :max="Math.max(0, reader.state.pageDimensions.length - 1)"
                @update:model-value="onSliderChange"
            />

            <section>
                <ReaderHeading>Display</ReaderHeading>
                <div class="text-fg-muted mb-2 text-sm" aria-hidden="true">Mode</div>
                <ASegmented
                    :model-value="reader.seriesSettings.mode ?? 'auto'"
                    :options="MODE_OPTIONS"
                    label="Mode"
                    block
                    @update:model-value="mode => reader.setMode(mode === 'auto' ? null : mode)"
                />
                <div v-if="reader.seriesSettings.mode == null" class="text-fg-muted mt-1 text-xs">
                    Auto: {{ reader.mode === 'longstrip' ? 'Longstrip' : 'Paged' }}
                </div>
                <ASlider
                    v-if="reader.mode === 'longstrip'"
                    class="mt-4"
                    :model-value="reader.settings.longstripWidth"
                    label="Width"
                    show-value
                    :format-value="width => `${width}%`"
                    :min="10"
                    :max="100"
                    :step="5"
                    @update:model-value="setLongstripWidth"
                />
            </section>

            <section class="text-sm">
                <ReaderHeading>Keyboard shortcuts</ReaderHeading>
                <div
                    v-for="[keys, action] in kbShortcuts"
                    :key="keys"
                    class="flex justify-between py-0.5 text-xs"
                >
                    <span>{{ action }}</span>
                    <span class="text-fg-muted font-mono">{{ keys }}</span>
                </div>
            </section>
        </div>
    </ADrawer>
</template>

<script setup lang="ts">
import { useDebounceFn } from '@vueuse/core'
import { computed, onUnmounted, ref, watch, type Ref } from 'vue'
import { useLayoutStore } from '@/pages/_layout/useLayoutStore'
import ACombobox from '@/ui/ACombobox.vue'
import ADrawer from '@/ui/ADrawer.vue'
import AIconButton from '@/ui/AIconButton.vue'
import ASegmented from '@/ui/ASegmented.vue'
import ASkeleton from '@/ui/ASkeleton.vue'
import ASlider from '@/ui/ASlider.vue'
import { IconChevronLeft, IconChevronRight, IconClose } from '@/ui/icons'
import type { Option } from '@/ui/options'
import { contentApi } from '@/utils/api/content'
import type { Content } from '@/utils/api/types'
import ReaderHeading from '../ReaderHeading.vue'
import { COMIC_SHORTCUTS as kbShortcuts } from '../shortcuts'
import type { ReaderMode } from './types'
import { useReaderStore } from './useComicDisplayStore'

const reader = useReaderStore()
const layout = useLayoutStore()

// Reka can't take `null` as an item value, so "Auto" (no series setting) is 'auto' here.
const MODE_OPTIONS = [
    { value: 'paged', label: 'Paged' },
    { value: 'longstrip', label: 'Longstrip' },
    { value: 'auto', label: 'Auto' },
] as const satisfies readonly Option<ReaderMode | 'auto'>[]

const chapterOptions = computed(
    () => reader.siblings?.items.map(item => ({ value: item.id, label: item.title })) ?? []
)

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
