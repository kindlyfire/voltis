<template>
    <div
        class="book-reader flex flex-col"
        :style="readerVars"
        @pointerdown="controls.handlePointerDown"
        @click="controls.handleClick"
    >
        <div class="flex flex-1 flex-col">
            <VAlert
                v-if="session?.notice"
                type="info"
                variant="tonal"
                density="compact"
                closable
                class="m-4"
                @click.stop
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
                    <VBtn variant="tonal" @click.stop="leaveStandalone">
                        <VIcon start>mdi-arrow-left</VIcon>
                        Back to reading
                    </VBtn>
                </div>
                <div v-else-if="ready && session.prevPage" class="flex justify-center p-4">
                    <VBtn variant="tonal" @click.stop="turn($event, -1)">
                        Previous: {{ session.prevPage.title }}
                    </VBtn>
                </div>

                <!-- Keyed: preserved through this book's loading and errors,
                replaced when the book itself changes. -->
                <div ref="host" :key="session.contentId" class="flex-1" />

                <template v-if="ready && !session.standalone">
                    <VDivider />
                    <div class="flex justify-center p-4">
                        <VBtn v-if="session.nextPage" color="primary" @click.stop="turn($event, 1)">
                            Next: {{ session.nextPage.title }}
                        </VBtn>
                        <span v-else class="text-sm opacity-60">End of book</span>
                    </div>
                    <div ref="sentinel" class="h-px" />
                </template>
            </template>
        </div>
    </div>

    <BookReaderDrawer :content-id="contentId" />

    <VProgressLinear
        :model-value="session?.percent ?? 0"
        class="reader-progress"
        height="3"
        color="primary"
    />
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useReaderTutorial } from '../useReaderTutorial'
import BookReaderDrawer from './BookReaderDrawer.vue'
import { FONT_STACKS, type BookFont } from './bookSettings'
import { useBookControls } from './useBookControls'
import { useBookDisplayStore } from './useBookDisplayStore'

defineProps<{ contentId: string }>()

const store = useBookDisplayStore()
const session = computed(() => store.session)
const ready = computed(() => !!session.value && !session.value.loading && !session.value.error)
const host = ref<HTMLDivElement>()
const sentinel = ref<HTMLDivElement>()
const controls = useBookControls()

useReaderTutorial(
    'book',
    computed(() => ready.value && !!session.value?.firstPageMounted)
)

const publisherFonts = computed(() => store.settings.fontFamily === 'publisher')

/** Custom properties inherit into the per-slice shadow roots. */
const readerVars = computed(() => ({
    '--reader-font-size': `${store.settings.fontSize}rem`,
    '--reader-font-family': publisherFonts.value
        ? undefined
        : FONT_STACKS[store.settings.fontFamily as BookFont],
    '--reader-line-height': `${store.settings.lineHeight}`,
    '--reader-max-width': `calc(${store.settings.width}em + 4rem)`,
}))

/** Blurred: the button survives the route change with focus, where the next
 * Space would re-activate it instead of scrolling. */
function turn(event: MouseEvent, delta: number) {
    ;(event.currentTarget as HTMLElement).blur()
    const current = session.value
    current?.goToPage(current.pageIndex + delta)
}

function leaveStandalone(event: MouseEvent) {
    ;(event.currentTarget as HTMLElement).blur()
    void session.value?.closeStandalone()
}

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

// A session mounts its slices with whatever the setting is when it builds them.
watch(
    [session, publisherFonts],
    ([current, publisher]) => {
        current?.setPublisherFonts(publisher)
    },
    { immediate: true }
)

watch(
    () => store.settings,
    () => {
        session.value?.reflow()
    },
    { deep: true }
)

onUnmounted(() => {
    session.value?.setElements({ host: null, sentinel: null })
})
</script>

<style scoped>
.book-reader {
    min-height: calc(100dvh - var(--v-layout-top, 0px));
}

.reader-progress {
    position: fixed;
    bottom: 0 !important;
    top: auto !important;
    left: 0;
    right: 0;
    z-index: 10000;
    pointer-events: none;
}
</style>
