<template>
    <section
        class="a-card"
        :class="[`variant-${variant}`, `padding-${padding}`, { linked: to != null }]"
    >
        <header v-if="title || $slots.title || $slots.headerActions" class="a-card__header">
            <component :is="`h${headingLevel}`" v-if="title || $slots.title" class="a-card__title">
                <RouterLink v-if="to != null" :to="to" class="a-card__link">
                    <slot name="title">{{ title }}</slot>
                </RouterLink>
                <slot v-else name="title">{{ title }}</slot>
            </component>
            <div v-if="$slots.headerActions" class="a-card__header-actions">
                <slot name="headerActions" />
            </div>
        </header>
        <slot />
        <footer v-if="$slots.actions" class="a-card__actions">
            <slot name="actions" />
        </footer>
    </section>
</template>

<script setup lang="ts">
import { RouterLink, type RouteLocationRaw } from 'vue-router'

/**
 * With `to`, the title becomes a link stretched over the whole card, so other buttons and
 * links inside the card keep working (they sit above it).
 */
withDefaults(
    defineProps<{
        variant?: 'raised' | 'tonal'
        padding?: 'none' | 'md' | 'lg'
        title?: string
        /** 3 inside a dialog, whose own title is the h2. */
        headingLevel?: 2 | 3
        to?: RouteLocationRaw
    }>(),
    { variant: 'raised', padding: 'lg', headingLevel: 2 }
)
</script>

<style scoped>
@layer ui {
    .a-card {
        position: relative;
        display: flex;
        flex-direction: column;
        gap: 10px;
        min-width: 0;
        border-radius: var(--radius-card);
        color: var(--color-fg);
    }

    .variant-raised {
        background: var(--color-raised);
        box-shadow: var(--shadow-card);
    }

    .variant-tonal {
        background: var(--color-surface-2);
    }

    .padding-md {
        padding: 14px 16px;
    }

    .padding-lg {
        padding: 20px 22px;
    }

    .padding-none {
        overflow: hidden;
    }

    .a-card__header {
        display: flex;
        align-items: center;
        gap: 8px;
        min-height: 28px;
    }

    /* The design's title-to-content gap (14px) on settings-style cards. */
    .padding-lg > .a-card__header {
        margin-bottom: 4px;
    }

    .a-card__title {
        flex: 1;
        min-width: 0;
        font-family: var(--font-display);
        font-size: 20px;
        font-weight: 600;
        line-height: 1.3;
    }

    .a-card__header-actions {
        display: flex;
        gap: 4px;
        margin-block: -6px;
        margin-inline-end: -8px;
    }

    .a-card__actions {
        display: flex;
        flex-wrap: wrap;
        justify-content: flex-end;
        gap: 8px;
        margin-top: 6px;
    }

    .a-card__link {
        color: inherit;
        text-decoration: none;
        outline: none;

        &::after {
            content: '';
            position: absolute;
            inset: 0;
            z-index: 0;
            border-radius: var(--radius-card);
        }

        &:focus-visible::after {
            outline: 2px solid var(--color-primary);
            outline-offset: 2px;
        }
    }

    /* `overflow: hidden` would clip an outset ring. */
    .padding-none .a-card__link:focus-visible::after {
        outline-offset: -2px;
    }

    /* Other interactive content stays above the stretched link. */
    .linked :deep(:is(a, button, input, select, textarea, label):not(.a-card__link)) {
        position: relative;
        z-index: 1;
    }

    .linked {
        transition: box-shadow var(--duration-short) var(--ease-standard);

        &:hover {
            box-shadow: var(--shadow-card), var(--shadow-button-hover);
        }

        &:hover .a-card__link {
            text-decoration: underline;
            text-underline-offset: 3px;
        }
    }
}
</style>
