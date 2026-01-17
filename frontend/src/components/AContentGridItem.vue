<template>
    <div>
        <VCard class="relative content-grid-item">
            <RouterLink :to="to" class="block" :title="content.title">
                <img
                    :src="coverUri ?? ''"
                    :style="{
                        aspectRatio: '2 / 3',
                        objectFit: 'cover',
                        width: '100%',
                    }"
                />
            </RouterLink>

            <span
                v-if="content.user_data?.status"
                class="absolute top-2 left-2 bg-black/80 text-white p-1 rounded-full aspect-square w-5 flex items-center justify-center"
                :title="`Status: ${STATUS_TITLES[content.user_data.status]}`"
            >
                <VIcon :icon="statusIcon" size="12" />
            </span>

            <span
                v-if="childrenCount != null"
                class="absolute top-2 right-2 bg-black/80 text-white text-xs font-medium px-2 py-0.5 rounded-full"
            >
                {{ childrenCount }}
            </span>

            <span
                v-if="toReadRoute"
                class="absolute bottom-2 right-2 bottom-actions flex items-center gap-1"
            >
                <RouterLink
                    :to="`/${content.id}`"
                    class="bg-black/80! p-1.5! rounded-full flex items-center justify-center text-white"
                    :title="`Go to content page`"
                >
                    <VIcon icon="mdi-information" size="16" />
                </RouterLink>
            </span>
        </VCard>

        <div class="text-body-2 pt-2 line-clamp-2">{{ content.title }}</div>
    </div>
</template>

<script setup lang="ts">
import type { Content, ReadingStatus } from '@/utils/api/types'
import { computed } from 'vue'
import { API_URL } from '@/utils/fetch'

const props = defineProps<{
    content: Content
    toReadRoute?: boolean
}>()

const STATUS_ICONS: Record<ReadingStatus, string> = {
    reading: 'mdi-book-open-page-variant',
    completed: 'mdi-check',
    on_hold: 'mdi-pause',
    dropped: 'mdi-close',
    plan_to_read: 'mdi-bookmark',
}

const STATUS_TITLES: Record<ReadingStatus, string> = {
    reading: 'Reading',
    completed: 'Completed',
    on_hold: 'On Hold',
    dropped: 'Dropped',
    plan_to_read: 'Plan to Read',
}

const to = computed(() =>
    props.toReadRoute ? `/r/${props.content.id}?page=resume` : `/${props.content.id}`
)

const coverUri = computed(() => {
    if (!props.content.cover_uri) return null
    return `${API_URL}/files/cover/${props.content.id}?v=${props.content.file_mtime}`
})

const childrenCount = computed(() => {
    return props.content.type === 'book_series' || props.content.type === 'comic_series'
        ? props.content.children_count
        : null
})

const statusIcon = computed(() => {
    if (!props.content.user_data?.status) return
    return STATUS_ICONS[props.content.user_data.status]
})
</script>

<style lang="css" scoped>
@media (hover: hover) and (pointer: fine) {
    .content-grid-item .bottom-actions {
        transition: opacity 0.1s;
        opacity: 0;
    }

    .content-grid-item:hover .bottom-actions {
        opacity: 1;
    }
}
</style>
