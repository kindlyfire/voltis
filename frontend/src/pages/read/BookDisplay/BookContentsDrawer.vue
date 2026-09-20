<template>
    <VNavigationDrawer
        v-model="store.sidebarOpen"
        temporary
        disable-route-watcher
        location="right"
        width="350"
        :style="{ top: '0', height: '100vh' }"
    >
        <div class="mt-16 p-4 pt-0">
            <div class="mb-2 flex h-16 items-center">
                <span class="text-h6">Contents</span>
                <VSpacer />
                <VBtn icon variant="text" @click="store.sidebarOpen = false">
                    <VIcon>mdi-close</VIcon>
                </VBtn>
            </div>

            <BookContents
                v-if="session?.structure"
                :content-id="session.contentId"
                :structure="session.structure"
                :entry-pages="session.entryPages"
                :active-page="session.pageIndex"
                :active-href="session.page?.target.href ?? null"
                :fallback="session.fallback"
                @select="onSelect"
            />
            <VSkeletonLoader v-else type="list-item@6" />
        </div>
    </VNavigationDrawer>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { useLayoutStore } from '@/pages/_layout/useLayoutStore'
import BookContents from './BookContents.vue'
import { useBookDisplayStore } from './useBookDisplayStore'

const store = useBookDisplayStore()
const layout = useLayoutStore()
const session = computed(() => store.session)

const navbarHidden = layout.navbarHidden.useLayer('bookReaderSidebar')
watch(
    () => store.sidebarOpen,
    open => {
        navbarHidden(open ? false : undefined)
    }
)

function onSelect() {
    store.session?.snapshotPassage()
    store.sidebarOpen = false
}
</script>
