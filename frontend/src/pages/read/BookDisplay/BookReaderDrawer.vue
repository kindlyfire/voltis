<template>
    <ADrawer v-slot="{ titleId }" v-model:open="store.sidebarOpen" side="right" :width="350">
        <div class="space-y-4 p-4 pt-0">
            <div class="mb-2 flex h-16 items-center gap-2">
                <AIconButton
                    :icon="IconArrowLeft"
                    label="Back to the book"
                    :to="`/${contentId}`"
                    @click="onLeave"
                />
                <h2 :id="titleId" class="font-display min-w-0 grow truncate text-xl font-semibold">
                    {{ session?.content?.title || 'Book' }}
                </h2>
                <AIconButton :icon="IconClose" label="Close" @click="store.sidebarOpen = false" />
            </div>

            <div v-if="session?.pages.length">
                <div class="mb-2 flex items-center gap-2">
                    <AIconButton
                        :icon="IconChevronLeft"
                        label="Previous chapter"
                        variant="tonal"
                        size="sm"
                        :disabled="!session.prevPage"
                        @click="session.goToPage(session.pageIndex - 1)"
                    />
                    <div class="grow truncate text-center text-sm">{{ session.title }}</div>
                    <AIconButton
                        :icon="IconChevronRight"
                        label="Next chapter"
                        variant="tonal"
                        size="sm"
                        :disabled="!session.nextPage"
                        @click="session.goToPage(session.pageIndex + 1)"
                    />
                </div>
                <div class="text-fg-muted text-center text-sm">
                    Chapter {{ session.pageIndex + 1 }} of {{ session.pages.length }} —
                    {{ Math.round(session.percent) }}%
                </div>
            </div>

            <section>
                <ReaderHeading>Contents</ReaderHeading>
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
                <ASkeleton v-else shape="text" :lines="6" />
            </section>

            <section>
                <ReaderHeading>Text</ReaderHeading>
                <BookSettings />
            </section>

            <section class="text-sm">
                <ReaderHeading>Keyboard shortcuts</ReaderHeading>
                <div
                    v-for="[keys, action] in kbShortcuts"
                    :key="keys"
                    class="flex justify-between gap-4 py-0.5 text-xs"
                >
                    <span class="shrink-0">{{ action }}</span>
                    <span class="text-fg-muted text-right font-mono">{{ keys }}</span>
                </div>
            </section>
        </div>
    </ADrawer>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { useLayoutStore } from '@/pages/_layout/useLayoutStore'
import ADrawer from '@/ui/ADrawer.vue'
import AIconButton from '@/ui/AIconButton.vue'
import ASkeleton from '@/ui/ASkeleton.vue'
import { IconArrowLeft, IconChevronLeft, IconChevronRight, IconClose } from '@/ui/icons'
import ReaderHeading from '../ReaderHeading.vue'
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
