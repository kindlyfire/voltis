<template>
    <div class="book-reader flex flex-col">
        <VAppBar density="compact" class="shrink-0">
            <VBtn icon :to="`/${contentId}`" variant="text" exact>
                <VIcon>mdi-arrow-left</VIcon>
            </VBtn>
            <VAppBarTitle class="text-base">{{ session?.title || 'Book' }}</VAppBarTitle>
            <VSpacer />
            <span class="mr-2 text-sm opacity-60">{{ Math.round(session?.percent ?? 0) }}%</span>
            <VBtn icon variant="text" @click="store.sidebarOpen = true">
                <VIcon>mdi-format-list-bulleted</VIcon>
            </VBtn>
        </VAppBar>

        <div class="flex flex-1 flex-col">
            <VAlert
                v-if="session?.notice"
                type="info"
                variant="tonal"
                density="compact"
                closable
                class="m-4"
                @click:close="session.dismissNotice()"
            >
                {{ session.notice }}
            </VAlert>

            <div v-if="!session || session.loading" class="flex justify-center py-8">
                <VProgressCircular indeterminate />
            </div>
            <VAlert v-else-if="session.error" type="error" variant="tonal" class="m-4">
                {{ session.error }}
            </VAlert>

            <template v-if="session">
                <div v-if="ready && session.standalone" class="flex justify-center p-4">
                    <VBtn variant="tonal" @click="session.closeStandalone()">
                        <VIcon start>mdi-arrow-left</VIcon>
                        Back to reading
                    </VBtn>
                </div>
                <div v-else-if="ready && session.prevPage" class="flex justify-center p-4">
                    <VBtn variant="tonal" @click="session.goToPage(session.pageIndex - 1)">
                        Previous: {{ session.prevPage.title }}
                    </VBtn>
                </div>

                <!-- Keyed: preserved through this book's loading and errors,
                replaced when the book itself changes. -->
                <div ref="host" :key="session.contentId" class="flex-1" />

                <template v-if="ready && !session.standalone">
                    <VDivider />
                    <div class="flex justify-center p-4">
                        <VBtn
                            v-if="session.nextPage"
                            color="primary"
                            @click="session.goToPage(session.pageIndex + 1)"
                        >
                            Next: {{ session.nextPage.title }}
                        </VBtn>
                        <span v-else class="text-sm opacity-60">End of book</span>
                    </div>
                    <div ref="sentinel" class="h-px" />
                </template>
            </template>
        </div>

        <BookContentsDrawer />
    </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import BookContentsDrawer from './BookContentsDrawer.vue'
import { useBookDisplayStore } from './useBookDisplayStore'

defineProps<{ contentId: string }>()

const store = useBookDisplayStore()
const session = computed(() => store.session)
const ready = computed(() => !!session.value && !session.value.loading && !session.value.error)
const host = ref<HTMLDivElement>()
const sentinel = ref<HTMLDivElement>()

watch(
    [session, host, sentinel],
    () => {
        session.value?.setElements({
            host: host.value ?? null,
            sentinel: sentinel.value ?? null,
        })
    },
    { immediate: true, flush: 'post' }
)

onUnmounted(() => {
    session.value?.setElements({ host: null, sentinel: null })
})
</script>

<style scoped>
.book-reader {
    min-height: calc(100dvh - var(--v-layout-top, 0px));
}
</style>
