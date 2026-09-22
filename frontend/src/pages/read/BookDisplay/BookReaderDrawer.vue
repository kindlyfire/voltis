<template>
    <VNavigationDrawer
        v-model="store.sidebarOpen"
        temporary
        disable-route-watcher
        location="right"
        width="350"
        :style="{ top: '0', height: '100vh' }"
    >
        <div class="mt-16 space-y-4! p-4 pt-0">
            <div class="mb-2! flex h-16 items-center gap-2">
                <VBtn icon variant="text" :to="`/${contentId}`" exact @click="onLeave">
                    <VIcon>mdi-arrow-left</VIcon>
                </VBtn>
                <span class="text-h6 truncate">{{ session?.content?.title || 'Book' }}</span>
                <VSpacer />
                <VBtn icon variant="text" @click="store.sidebarOpen = false">
                    <VIcon>mdi-close</VIcon>
                </VBtn>
            </div>

            <div v-if="session?.pages.length">
                <div class="mb-2 flex items-center gap-2">
                    <VBtn
                        icon
                        size="small"
                        variant="tonal"
                        :disabled="!session.prevPage"
                        @click="session.goToPage(session.pageIndex - 1)"
                    >
                        <VIcon>mdi-chevron-left</VIcon>
                    </VBtn>
                    <div class="grow truncate text-center text-sm">{{ session.title }}</div>
                    <VBtn
                        icon
                        size="small"
                        variant="tonal"
                        :disabled="!session.nextPage"
                        @click="session.goToPage(session.pageIndex + 1)"
                    >
                        <VIcon>mdi-chevron-right</VIcon>
                    </VBtn>
                </div>
                <div class="text-center text-sm opacity-60">
                    Chapter {{ session.pageIndex + 1 }} of {{ session.pages.length }} —
                    {{ Math.round(session.percent) }}%
                </div>
            </div>

            <BookContents
                v-if="session?.structure"
                :content-id="session.contentId"
                :structure="session.structure"
                :entry-pages="session.entryPages"
                :active-page="session.pageIndex"
                :active-href="session.page?.target.href ?? null"
                :fallback="session.fallback"
                @select="session?.snapshotPassage()"
            />
            <VSkeletonLoader v-else type="list-item@6" />

            <VDivider />

            <BookSettings />

            <div class="text-sm opacity-60">
                <div class="mb-1">Keyboard shortcuts</div>
                <div
                    v-for="[keys, action] in kbShortcuts"
                    :key="keys"
                    class="flex justify-between gap-4 text-xs!"
                >
                    <span class="shrink-0">{{ action }}</span>
                    <span class="text-right font-mono">{{ keys }}</span>
                </div>
            </div>
        </div>
    </VNavigationDrawer>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { useLayoutStore } from '@/pages/_layout/useLayoutStore'
import { BOOK_SHORTCUTS, type Shortcut } from '../shortcuts'
import BookContents from './BookContents.vue'
import BookSettings from './BookSettings.vue'
import { useBookDisplayStore } from './useBookDisplayStore'

defineProps<{ contentId: string }>()

const store = useBookDisplayStore()
const layout = useLayoutStore()
const session = computed(() => store.session)

const kbShortcuts: Shortcut[] = [...BOOK_SHORTCUTS, ['Escape', 'Close this panel']]

const navbarHidden = layout.navbarHidden.useLayer('bookReaderSidebar')
watch(
    () => store.sidebarOpen,
    open => {
        navbarHidden(open ? false : undefined)
    }
)

function onLeave() {
    store.session?.snapshotPassage()
    store.sidebarOpen = false
}
</script>
