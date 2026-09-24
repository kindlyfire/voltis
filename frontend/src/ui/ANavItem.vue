<template>
    <RouterLink v-if="to != null && !disabled" v-slot="link" :to="to" custom>
        <a
            v-bind="$attrs"
            :href="link.href"
            class="a-nav-item a-state a-focus"
            :class="classes(isActive(link))"
            :aria-current="isActive(link) ? 'page' : undefined"
            @click="link.navigate"
        >
            <span v-if="$slots.prefix" class="a-nav-item__prefix"><slot name="prefix" /></span>
            <AIcon
                v-if="icon"
                :icon="(isActive(link) && activeIcon) || icon"
                class="a-nav-item__icon"
            />
            <span class="a-nav-item__label"
                ><slot>{{ label }}</slot></span
            >
        </a>
    </RouterLink>
    <button
        v-else
        v-bind="$attrs"
        type="button"
        class="a-nav-item a-state a-focus"
        :class="classes(!!active)"
        :aria-current="active ? 'page' : undefined"
        :disabled="disabled"
    >
        <span v-if="$slots.prefix" class="a-nav-item__prefix"><slot name="prefix" /></span>
        <AIcon v-if="icon" :icon="(active && activeIcon) || icon" class="a-nav-item__icon" />
        <span class="a-nav-item__label"
            ><slot>{{ label }}</slot></span
        >
    </button>
</template>

<script setup lang="ts">
import type { Component } from 'vue'
import { RouterLink, type RouteLocationRaw } from 'vue-router'
import AIcon from './AIcon.vue'

/** A sidebar or drawer entry: a link with `to`, else a button (`@click`, or a menu trigger). */
defineOptions({ inheritAttrs: false })

const props = withDefaults(
    defineProps<{
        to?: RouteLocationRaw
        label?: string
        icon?: Component
        activeIcon?: Component
        /** Active only on an exact route match. */
        exact?: boolean
        /** Overrides the route-based active state. */
        active?: boolean
        disabled?: boolean
        /** Tree depth (table of contents). */
        indent?: number
        /** Lets long labels wrap instead of truncating. */
        wrap?: boolean
    }>(),
    { active: undefined, indent: 0 }
)

function isActive(link: { isActive: boolean; isExactActive: boolean }) {
    return props.active ?? (props.exact ? link.isExactActive : link.isActive)
}

function classes(active: boolean) {
    return { active, wrap: props.wrap }
}
</script>

<style scoped>
@layer ui {
    .a-nav-item {
        display: flex;
        align-items: center;
        gap: 14px;
        width: 100%;
        min-height: 44px;
        padding: 0 16px 0 calc(16px + v-bind(indent) * 16px);
        border: 0;
        border-radius: 12px;
        background: transparent;
        color: var(--color-fg);
        font-size: 14px;
        font-weight: 500;
        text-align: start;
        text-decoration: none;
        cursor: pointer;
        transition: background-color var(--duration-short) var(--ease-standard);
    }

    .a-nav-item__icon {
        font-size: 22px;
        color: var(--color-fg-muted);
    }

    .a-nav-item__prefix {
        display: flex;
        flex: none;
        align-items: center;
        min-width: 22px;
        color: var(--color-fg-muted);
        font-variant-numeric: tabular-nums;
    }

    .a-nav-item__label {
        flex: 1;
        min-width: 0;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .wrap {
        padding-block: 10px;
        line-height: 1.4;

        & .a-nav-item__label {
            white-space: normal;
            overflow-wrap: anywhere;
        }
    }

    .active {
        background: var(--color-secondary-container);
        color: var(--color-primary);
        font-weight: 600;

        & .a-nav-item__icon {
            color: var(--color-primary);
        }
    }

    .a-nav-item:focus-visible {
        outline-offset: -2px;
    }

    .a-nav-item:disabled {
        cursor: not-allowed;
        color: color-mix(in oklch, var(--color-fg) 38%, transparent);

        & .a-nav-item__icon {
            color: inherit;
        }
    }
}
</style>
