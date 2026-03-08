<template>
    <div class="AContentGridItem">
        <VCard class="content-grid-item relative">
            <component
                :is="selecting ? 'div' : RouterLink"
                :to="selecting ? undefined : to"
                class="block cursor-pointer"
                :title="content.title"
                @click="selecting && emit('toggleSelect', $event.shiftKey)"
            >
                <img
                    :src="coverUri ?? ''"
                    :style="{
                        aspectRatio: '2 / 3',
                        objectFit: 'cover',
                        width: '100%',
                    }"
                    class="block"
                />
            </component>

            <span
                v-if="content.user_data?.status && !settings.hideStatus"
                class="absolute top-2 left-2 flex aspect-square w-5 items-center justify-center rounded-full bg-black/80 p-1 text-white"
                :title="`Status: ${READING_STATUS_LABELS[content.user_data.status]}`"
            >
                <VIcon :icon="statusIcon" size="12" />
            </span>

            <span
                v-if="childrenCount != null && !settings.hideItemCount"
                class="absolute top-2 right-2 rounded-full bg-black/80 px-2 py-0.5 text-xs font-medium text-white"
            >
                {{ childrenCount }}
            </span>

            <div class="absolute bottom-0 left-0 rounded-tr-md bg-white/70 p-1 dark:bg-black/60">
                <VCheckboxBtn
                    v-if="selecting"
                    :model-value="selected"
                    density="compact"
                    @click.stop="emit('toggleSelect', $event.shiftKey)"
                />
            </div>

            <span
                v-if="toReadRoute"
                class="bottom-actions absolute right-2 bottom-2 flex items-center gap-1"
            >
                <RouterLink
                    :to="`/${content.id}`"
                    class="flex items-center justify-center rounded-full bg-black/80! p-1.5! text-white"
                    :title="`Go to content page`"
                >
                    <VIcon icon="mdi-information" size="16" />
                </RouterLink>
            </span>
        </VCard>

        <div v-if="!settings.hideTitle" class="text-body-2 line-clamp-2 pt-2">
            {{ content.title }}
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, toRef } from 'vue'
import { RouterLink } from 'vue-router'
import { READING_STATUS_LABELS } from '@/utils/api/types'
import type { Content, ReadingStatus } from '@/utils/api/types'
import { API_URL } from '@/utils/fetch'
import { useContentGridStore } from './store'

const props = withDefaults(
    defineProps<{
        content: Content
        toReadRoute?: boolean
        storeKey?: string
        selecting?: boolean
        selected?: boolean
    }>(),
    { storeKey: 'default' }
)

const emit = defineEmits<{
    toggleSelect: [shiftKey: boolean]
}>()

const store = useContentGridStore()
const settings = store.getForKey(toRef(props, 'storeKey'))

const STATUS_ICONS: Record<ReadingStatus, string> = {
    reading: 'mdi-book-open-page-variant',
    completed: 'mdi-check',
    on_hold: 'mdi-pause',
    dropped: 'mdi-close',
    plan_to_read: 'mdi-bookmark',
}

const to = computed(() =>
    props.toReadRoute ? `/r/${props.content.id}?page=resume` : `/${props.content.id}`
)

const coverUri = computed(() => {
    if (!props.content.cover_uri) return null
    return `${API_URL}/files/cover/${props.content.id}?v=${props.content.file_mtime}`
})

const childrenCount = computed(() => {
    if (props.content.type !== 'book_series' && props.content.type !== 'comic_series') return null
    const count =
        settings.value.itemCountMode === 'unread'
            ? props.content.unread_children_count
            : props.content.children_count
    if (settings.value.itemCountMode === 'unread' && count === 0) return null
    return count
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
