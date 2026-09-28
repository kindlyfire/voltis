<template>
    <div class="content-card" :class="{ selecting, selected }">
        <!-- First, so the progress bar is read after it. -->
        <RouterLink v-if="!selecting" :to="to" class="content-card__link" :aria-label="linkLabel" />
        <ACover
            :src="coverUri"
            alt=""
            :progress="!settings.hideProgress && progress ? progress.fraction : undefined"
            :progress-text="progress?.label"
            class="content-card__cover"
        >
            <template v-if="statusLabel && !settings.hideStatus && !selecting" #topLeft>
                <!-- Named by the card's link. -->
                <ABadge :icon="statusIcon" :label="statusLabel" aria-hidden="true" />
            </template>
            <template v-if="childrenCount != null && !settings.hideItemCount" #topRight>
                <ABadge aria-hidden="true">{{ childrenCount }}</ABadge>
            </template>
            <span v-if="toReadRoute && !selecting" class="content-card__details">
                <AIconButton
                    :to="`/${(series ?? content).id}`"
                    :icon="IconInformation"
                    :label="`Details for ${title}`"
                    variant="tonal"
                    size="sm"
                />
            </span>
        </ACover>

        <!-- Above the card link so it can show the full text; the card link names it. -->
        <ATooltip v-if="!settings.hideTitle" :disabled="selecting || !truncated">
            <RouterLink
                :to="to"
                tabindex="-1"
                aria-hidden="true"
                class="content-card__text"
                @pointerenter="checkTruncated"
            >
                <div class="content-card__title">{{ title }}</div>
                <div v-if="subtitle" class="content-card__subtitle">{{ subtitle }}</div>
            </RouterLink>
            <template #content>
                <div>{{ title }}</div>
                <div v-if="subtitle" class="content-card__tooltip-subtitle">{{ subtitle }}</div>
            </template>
        </ATooltip>

        <ACheckbox
            v-if="selecting"
            :model-value="!!selected"
            :label="`Select ${content.title}`"
            hide-label
            class="content-card__select"
            @click="emit('toggleSelect', $event.shiftKey)"
        />
    </div>
</template>

<script setup lang="ts">
import { computed, ref, toRef } from 'vue'
import { RouterLink } from 'vue-router'
import ABadge from '@/ui/ABadge.vue'
import ACheckbox from '@/ui/ACheckbox.vue'
import ACover from '@/ui/ACover.vue'
import AIconButton from '@/ui/AIconButton.vue'
import ATooltip from '@/ui/ATooltip.vue'
import {
    IconBookmarkFilled,
    IconBookOpenFilled,
    IconCheck,
    IconClose,
    IconInformation,
    IconPauseFilled,
} from '@/ui/icons'
import { coverUrl } from '@/utils/api/content'
import { READING_STATUS_LABELS } from '@/utils/api/types'
import type { Content, ReadingStatus } from '@/utils/api/types'
import { contentProgress, seriesPosition } from '@/utils/contentProgress'
import { useContentGridStore } from './store'

const props = withDefaults(
    defineProps<{
        content: Content
        /** Shows `content` as the item to read next in this series. */
        series?: Content | null
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

const STATUS_ICONS = {
    reading: IconBookOpenFilled,
    completed: IconCheck,
    on_hold: IconPauseFilled,
    dropped: IconClose,
    plan_to_read: IconBookmarkFilled,
} satisfies Record<ReadingStatus, unknown>

const to = computed(() =>
    props.toReadRoute ? `/r/${props.content.id}?page=resume` : `/${props.content.id}`
)

const title = computed(() => (props.series ?? props.content).title)
const position = computed(() => (props.series ? seriesPosition(props.series) : null))
const subtitle = computed(() => {
    if (!props.series) return undefined
    const p = position.value
    return p ? `${props.content.title} · ${p.read} / ${p.total}` : props.content.title
})

// Measured on pointerenter, which fires before the pointermove that opens the tooltip.
const truncated = ref(false)
function checkTruncated(e: PointerEvent) {
    truncated.value = [...(e.currentTarget as HTMLElement).children].some(
        el => el.scrollHeight > el.clientHeight || el.scrollWidth > el.clientWidth
    )
}

const coverUri = computed(() => coverUrl(props.content))

const childrenCount = computed(() => {
    if (props.content.type !== 'book_series' && props.content.type !== 'comic_series') return null
    const count =
        settings.value.itemCountMode === 'unread'
            ? props.content.unread_children_count
            : props.content.children_count
    if (settings.value.itemCountMode === 'unread' && count === 0) return null
    return count
})

const progress = computed(() => contentProgress(props.content))

const status = computed(() => props.content.user_data?.status)
const statusIcon = computed(() => (status.value ? STATUS_ICONS[status.value] : undefined))
const statusLabel = computed(() => (status.value ? READING_STATUS_LABELS[status.value] : undefined))

// Deliberate, so the progress bar and badges don't end up in the name.
const linkLabel = computed(() => {
    const parts = [props.toReadRoute ? `Read ${title.value}` : title.value]
    if (props.series) {
        parts.push(props.content.title)
        if (position.value) parts.push(`${position.value.read} of ${position.value.total} read`)
    }
    if (statusLabel.value) parts.push(statusLabel.value)
    if (childrenCount.value != null) {
        const unread = settings.value.itemCountMode === 'unread'
        parts.push(`${childrenCount.value} ${unread ? 'unread' : 'items'}`)
    }
    return parts.join(', ')
})
</script>

<style scoped>
@layer ui {
    .content-card {
        position: relative;
        display: flex;
        flex-direction: column;
        gap: 10px;
        min-width: 0;
    }

    .content-card__cover {
        transition:
            box-shadow var(--duration-short) var(--ease-standard),
            outline-color var(--duration-short) var(--ease-standard);
        outline: 3px solid transparent;
        outline-offset: 2px;
    }

    .content-card:hover .content-card__cover {
        box-shadow: 0 6px 16px -6px oklch(0.2 0.02 60 / 0.35);
    }

    .content-card__text {
        display: block;
        position: relative;
        z-index: 2;
        color: inherit;
        text-decoration: none;
    }

    .selecting .content-card__text {
        z-index: auto;
    }

    .content-card__tooltip-subtitle {
        opacity: 0.75;
    }

    .content-card__title {
        display: -webkit-box;
        overflow: hidden;
        padding: 0 2px;
        font-size: 14px;
        font-weight: 500;
        line-height: 1.35;
        overflow-wrap: anywhere;
        -webkit-box-orient: vertical;
        -webkit-line-clamp: 2;
    }

    .content-card__subtitle {
        overflow: hidden;
        padding: 0 2px;
        color: var(--color-fg-muted);
        font-size: 13px;
        line-height: 1.35;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .content-card__link {
        position: absolute;
        inset: 0;
        z-index: 1;
        border-radius: var(--radius-cover);
        outline: none;

        &:focus-visible {
            outline: 2px solid var(--color-primary);
            outline-offset: 3px;
        }
    }

    .content-card:hover .content-card__title {
        text-decoration: underline;
        text-underline-offset: 3px;
    }

    .content-card__details {
        display: flex;
        position: absolute;
        right: 8px;
        bottom: 12px;
        z-index: 2;
    }

    @media (hover: hover) and (pointer: fine) {
        .content-card__details {
            opacity: 0;
            transition: opacity var(--duration-short) var(--ease-standard);
        }

        .content-card:is(:hover, :focus-within) .content-card__details {
            opacity: 1;
        }
    }

    /* Select mode: the whole card is the checkbox's label. */
    .content-card__select {
        position: absolute;
        inset: 0;
        z-index: 1;

        & :deep(.a-checkbox__row) {
            align-items: flex-start;
            height: 100%;
            padding: 12px;
        }

        & :deep(.a-checkbox__box) {
            border-radius: 6px;
            background: var(--color-raised);
            box-shadow:
                0 0 0 4px var(--color-raised),
                0 1px 6px 3px oklch(0 0 0 / 0.25);
        }

        /* Outside the halo. */
        & :deep(input:focus-visible) {
            outline-offset: 6px;
        }
    }

    .selected .content-card__cover {
        outline-color: var(--color-primary);
    }

    .selecting:not(.selected) .content-card__cover {
        opacity: 0.85;
    }

    .selecting {
        user-select: none;
    }

    .selecting:hover .content-card__title {
        text-decoration: none;
    }
}
</style>
