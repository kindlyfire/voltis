<template>
    <div class="a-cover">
        <img
            v-if="src && !failed"
            ref="img"
            :src="src"
            :alt="alt"
            :class="{ loaded, instant }"
            loading="lazy"
            decoding="async"
            @load="loaded = true"
            @error="failed = true"
        />
        <ASkeleton v-if="src && !loaded && !failed" class="a-cover__fill" />
        <div
            v-if="!src || failed"
            class="a-cover__fill a-cover__placeholder"
            :role="alt ? 'img' : undefined"
            :aria-label="alt || undefined"
        >
            <AIcon :icon="failed ? IconImageOff : IconBookOpen" />
        </div>
        <div v-if="$slots.topLeft" class="a-cover__corner top-left"><slot name="topLeft" /></div>
        <div v-if="$slots.topRight" class="a-cover__corner top-right"><slot name="topRight" /></div>
        <div v-if="$slots.bottomRight" class="a-cover__corner bottom-right">
            <slot name="bottomRight" />
        </div>
        <AProgressBar
            v-if="progress != null"
            class="a-cover__progress"
            variant="overlay"
            :value="progress"
            :label="progressLabel"
            :value-text="progressText"
        />
        <slot />
    </div>
</template>

<script setup lang="ts">
import { nextTick, ref, useTemplateRef, watch } from 'vue'
import AIcon from './AIcon.vue'
import AProgressBar from './AProgressBar.vue'
import ASkeleton from './ASkeleton.vue'
import { IconBookOpen, IconImageOff } from './icons'

/** A 2:3 cover image, with a skeleton while loading and a placeholder when missing or broken. */
const props = withDefaults(
    defineProps<{
        src?: string | null
        /** Empty when the title is shown next to the cover. */
        alt: string
        /** From 0 to 1: a thin bar along the bottom edge. */
        progress?: number
        progressLabel?: string
        progressText?: string
    }>(),
    { progressLabel: 'Reading progress' }
)

const loaded = ref(false)
const failed = ref(false)
const instant = ref(false)
const img = useTemplateRef('img')
// A cached image shows without the fade, on mount and when a reused cover gets a new `src`.
// `transition: none`, since a layout read before the check may already have started the fade.
watch(
    () => props.src,
    async () => {
        loaded.value = failed.value = instant.value = false
        await nextTick()
        if (img.value?.complete && img.value.naturalWidth > 0) loaded.value = instant.value = true
    },
    { immediate: true }
)
</script>

<style scoped>
@layer ui {
    .a-cover {
        position: relative;
        aspect-ratio: 2 / 3;
        overflow: hidden;
        border-radius: var(--radius-cover);
        background: var(--color-surface-3);
        box-shadow: 0 1px 2px oklch(0.2 0.02 60 / 0.12);
    }

    img {
        width: 100%;
        height: 100%;
        object-fit: cover;
        opacity: 0;
        transition: opacity var(--duration-medium) var(--ease-standard);

        &.loaded {
            opacity: 1;
        }

        &.instant {
            transition: none;
        }
    }

    .a-cover__fill {
        position: absolute;
        inset: 0;
        height: 100%;
        border-radius: 0;
    }

    .a-cover__placeholder {
        display: grid;
        place-items: center;
        color: var(--color-fg-muted);
        font-size: 40px;
    }

    .a-cover__corner {
        position: absolute;
        display: flex;
        gap: 4px;

        &.top-left {
            top: 8px;
            left: 8px;
        }

        &.top-right {
            top: 8px;
            right: 8px;
        }

        &.bottom-right {
            right: 8px;
            bottom: 8px;
            left: 8px;
            justify-content: flex-end;
        }
    }

    .a-cover__progress {
        position: absolute;
        right: 0;
        bottom: 0;
        left: 0;
    }
}
</style>
