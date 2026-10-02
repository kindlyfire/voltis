<template>
    <div v-if="reader.sync && content" class="comic-end-card flex flex-col items-center gap-4 p-8">
        <template v-if="next">
            <p :id="nextId" class="max-w-md text-center text-balance">{{ next.title }}</p>
            <AButton :aria-describedby="nextId" @click.stop="reader.goToSibling('next')">
                Read next
            </AButton>
        </template>
        <template v-else-if="reader.siblings.status === 'ready'">
            <h2 class="font-display text-2xl font-semibold">
                {{ content.parent_id ? 'End of the last chapter' : 'The end' }}
            </h2>
            <ReaderEndSummary :sync="reader.sync" :exit="exit" />
        </template>
        <ASpinner
            v-else-if="reader.siblings.status === 'loading'"
            label="Finding what comes next"
        />
        <SiblingsRetry v-else :siblings="reader.siblings" />
    </div>
</template>

<script setup lang="ts">
import { computed, useId } from 'vue'
import AButton from '@/ui/AButton.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ReaderEndSummary from '../ReaderEndSummary.vue'
import { readerExit } from '../readerExit'
import SiblingsRetry from '../SiblingsRetry.vue'
import { useReaderStore } from './useComicDisplayStore'

/** Past the end of a comic with nothing after it. */
const reader = useReaderStore()
const content = computed(() => reader.state?.content ?? null)
const next = computed(() => reader.siblings.next)
const nextId = useId()
const exit = computed(() =>
    readerExit(content.value?.parent_id, content.value?.id ?? '', 'Back to the comic')
)
</script>
