<template>
    <VNavigationDrawer
        v-model="reader.sidebarOpen"
        temporary
        disable-route-watcher
        location="right"
        width="300"
        :style="{
            top: '0',
            height: '100vh',
            zIndex: 1010,
        }"
    >
        <div class="pa-4 space-y-4!" v-if="reader.state">
            <div class="d-flex align-center">
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
                        class="font-weight-medium text-blue-400 hover:underline"
                    >
                        {{ parent.title }}
                    </RouterLink>
                </template>
            </div>

            <div v-if="reader.siblings">
                <div class="d-flex align-center gap-2 mb-2">
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
                <div class="text-body-2 text-medium-emphasis text-center">
                    {{ reader.siblings.currentIndex + 1 }} of
                    {{ reader.siblings.items.length }}
                </div>
            </div>

            <div>
                <div class="text-body-2 text-medium-emphasis mb-1">
                    Page {{ reader.state.page + 1 }} of
                    {{ reader.state.pageDimensions.length }}
                </div>
                <VSlider
                    :model-value="reader.state.page"
                    :min="0"
                    :max="Math.max(0, reader.state.pageDimensions.length - 1)"
                    :step="1"
                    hide-details
                    @update:model-value="reader.goToPage($event)"
                />
            </div>

            <div>
                <div class="text-body-2 text-medium-emphasis mb-2">Mode</div>
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
                    <div class="text-xs text-medium-emphasis mt-1">
                        Auto: {{ reader.mode === 'longstrip' ? 'Longstrip' : 'Paged' }}
                    </div>
                </template>
            </div>

            <div v-if="reader.mode === 'longstrip'">
                <div class="text-body-2 text-medium-emphasis mb-1">
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

            <div class="text-body-2 text-medium-emphasis">
                <div class="mb-1">Keyboard shortcuts</div>
                <div v-for="s in kbShortcuts" class="d-flex justify-space-between text-xs!">
                    <span>{{ s[1] }}</span>
                    <span class="font-mono">
                        {{ s[0] }}
                    </span>
                </div>
            </div>
        </div>
    </VNavigationDrawer>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch, type Ref } from 'vue'
import { useReaderStore } from './useComicDisplayStore'
import { contentApi } from '@/utils/api/content'
import type { Content } from '@/utils/api/types'
import type { ReaderMode } from './types'

const reader = useReaderStore()

const kbShortcuts = [
    ['Left arrow', 'Previous Page'],
    ['Right arrow', 'Next Page'],
    ['Comma', 'Previous Entry'],
    ['Period', 'Next Entry'],
]

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

onMounted(() => {
    console.log('Onmounted called')
})
</script>
