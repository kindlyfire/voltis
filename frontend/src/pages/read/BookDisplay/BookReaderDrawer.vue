<template>
    <ADrawer v-slot="{ titleId }" v-model:open="store.sidebarOpen" side="right" :width="350">
        <div class="shrink-0 px-4">
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

            <div v-if="session?.chapters.length">
                <div class="mb-2 flex items-center gap-2">
                    <AIconButton
                        :icon="IconChevronLeft"
                        label="Previous chapter"
                        variant="tonal"
                        size="sm"
                        :disabled="!session.prevChapter"
                        @click="session.goToChapter(session.chapterIndex - 1)"
                    />
                    <div class="grow truncate text-center text-sm">{{ session.title }}</div>
                    <AIconButton
                        :icon="IconChevronRight"
                        label="Next chapter"
                        variant="tonal"
                        size="sm"
                        :disabled="!session.nextChapter"
                        @click="session.goToChapter(session.chapterIndex + 1)"
                    />
                </div>
                <div class="text-fg-muted text-center text-sm">
                    Chapter {{ session.chapterIndex + 1 }} of {{ session.chapters.length }} &bull;
                    {{ Math.round(session.percent) }}%
                </div>
                <ASlider
                    v-if="pageCount > 1"
                    class="mt-4"
                    :model-value="sliderPage"
                    label="Page"
                    show-value
                    :format-value="i => `${i + 1} of ${pageCount}`"
                    :min="0"
                    :max="pageCount - 1"
                    @update:model-value="onSliderChange"
                />
            </div>
        </div>

        <ATabs
            ref="tabs"
            v-model="store.drawerTab"
            :options="TABS"
            label="Reader panel"
            class="mt-2"
        >
            <template #contents>
                <div class="p-4 pt-2">
                    <BookContents
                        v-if="session?.structure"
                        :content-id="session.contentId"
                        :structure="session.structure"
                        :entry-chapters="session.entryChapters"
                        :active-chapter="session.chapterIndex"
                        :active-href="session.chapter?.target.href ?? null"
                        :fallback="session.fallback"
                        @select="onContentsSelect"
                    />
                    <ASkeleton v-else shape="text" :lines="6" />
                </div>
            </template>
            <template #settings>
                <div class="space-y-6 p-4">
                    <section>
                        <ReaderHeading>Display</ReaderHeading>
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
            </template>
        </ATabs>
    </ADrawer>
</template>

<script setup lang="ts">
import { useDebounceFn } from '@vueuse/core'
import { computed, ref, useTemplateRef, watch } from 'vue'
import { useLayoutStore } from '@/pages/_layout/useLayoutStore'
import ADrawer from '@/ui/ADrawer.vue'
import AIconButton from '@/ui/AIconButton.vue'
import ASkeleton from '@/ui/ASkeleton.vue'
import ASlider from '@/ui/ASlider.vue'
import ATabs from '@/ui/ATabs.vue'
import { IconArrowLeft, IconChevronLeft, IconChevronRight, IconClose } from '@/ui/icons'
import ReaderHeading from '../ReaderHeading.vue'
import { bookShortcuts, type Shortcut } from '../shortcuts'
import BookContents from './BookContents.vue'
import BookSettings from './BookSettings.vue'
import { useBookDisplayStore, type DrawerTab } from './useBookDisplayStore'

defineProps<{ contentId: string }>()

const TABS: { value: DrawerTab; label: string }[] = [
    { value: 'contents', label: 'Contents' },
    { value: 'settings', label: 'Settings' },
]

const store = useBookDisplayStore()
const layout = useLayoutStore()
const session = computed(() => store.session)
const tabs = useTemplateRef('tabs')

const kbShortcuts = computed<Shortcut[]>(() => [
    ...bookShortcuts(session.value?.layoutMode ?? 'paged'),
    ['Escape', 'Close this panel'],
])

const navbarHidden = layout.navbarHidden.useLayer('bookReaderSidebar')
watch(
    () => store.sidebarOpen,
    open => {
        navbarHidden(open ? false : undefined)
    }
)

/** The mounted chapter's screens, while paged. */
const pageCount = computed(() => {
    const current = session.value
    return current?.layoutMode === 'paged' && current.firstChapterMounted
        ? (current.screen?.count ?? 0)
        : 0
})
const sliderPage = ref(session.value?.screen?.index ?? 0)
watch(
    () => session.value?.screen?.index,
    index => {
        if (index !== undefined) sliderPage.value = index
    }
)
const debouncedGoToScreen = useDebounceFn((index: number) => {
    session.value?.goToScreen(index)
}, 200)
function onSliderChange(index: number) {
    sliderPage.value = index
    debouncedGoToScreen(index)
}
/** A pending slider jump mustn't override the chosen entry. */
function onContentsSelect() {
    debouncedGoToScreen.cancel()
    session.value?.snapshotPassage()
}
// A pending jump mustn't land in the next chapter or book.
watch(
    [() => session.value?.chapterIndex, () => session.value?.contentId],
    () => {
        debouncedGoToScreen.cancel()
        sliderPage.value = session.value?.screen?.index ?? 0
    },
    { flush: 'post' }
)

/*
 * Tab scroll is kept in the store: the drawer's content unmounts on close. It's saved when a
 * panel is hidden (tab switch, close), while its element is still shown: `pre` runs before the
 * DOM update.
 */
let restorePending = false

function saveScroll(tab: DrawerTab) {
    const el = tabs.value?.panel(tab)
    if (el) store.drawerScroll[tab] = el.scrollTop
}

watch(
    () => store.sidebarOpen,
    open => {
        if (open) restorePending = true
        // Unrestored, its scroll isn't the user's (a skeleton's, or the previous book's).
        else if (!restorePending) saveScroll(store.drawerTab)
    }
)
watch(
    () => store.drawerTab,
    (_, prev) => {
        if (!restorePending) saveScroll(prev)
        restorePending = true
    }
)
watch(
    () => session.value?.contentId,
    () => {
        if (store.sidebarOpen) restorePending = true
    }
)
// The contents may render after the drawer opens, once the structure loads.
watch(
    [
        () => store.sidebarOpen,
        () => store.drawerTab,
        () => session.value?.contentId,
        () => !!session.value?.structure,
    ],
    () => {
        if (store.sidebarOpen && restorePending) restorePending = !restoreScroll(store.drawerTab)
    },
    { flush: 'post' }
)

/** Whether there was something to restore onto. */
function restoreScroll(tab: DrawerTab) {
    const el = tabs.value?.panel(tab)
    if (!el || (tab === 'contents' && !session.value?.structure)) return false
    const saved = store.drawerScroll[tab]
    if (saved != null) el.scrollTop = saved
    else if (tab === 'contents') centerActiveEntry(el)
    return true
}

/** First time in this book: show the current chapter rather than the top. Not `scrollIntoView`,
 * which would also scroll the (locked) page. */
function centerActiveEntry(panel: HTMLElement) {
    const entry = panel.querySelector('[aria-current="page"]')
    if (!entry) return
    const box = entry.getBoundingClientRect()
    const panelBox = panel.getBoundingClientRect()
    panel.scrollTop += box.top - panelBox.top - (panel.clientHeight - box.height) / 2
}

function onLeave() {
    store.session?.snapshotPassage()
    store.sidebarOpen = false
}
</script>
