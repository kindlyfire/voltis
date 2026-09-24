<template>
    <Teleport to="#overlays">
        <Transition name="a-drawer-scrim">
            <div
                v-if="open"
                class="a-drawer-scrim"
                :class="{ nav }"
                aria-hidden="true"
                @click="open = false"
            />
        </Transition>
        <Transition name="a-drawer">
            <div
                v-if="open"
                ref="panel"
                class="a-drawer"
                :class="[`side-${side}`, { nav }]"
                :style="{ '--drawer-width': typeof width === 'number' ? `${width}px` : width }"
                role="dialog"
                aria-modal="false"
                :aria-labelledby="titleId"
                tabindex="-1"
            >
                <h2 v-if="title" :id="titleId" class="sr-only">{{ title }}</h2>
                <slot :title-id="titleId" />
            </div>
        </Transition>
    </Teleport>
</template>

<script setup lang="ts">
import { useEventListener } from '@vueuse/core'
import { nextTick, useId, useTemplateRef, watch } from 'vue'
import { useOverlayLayer } from './overlay'

/**
 * A side sheet. Not modal: no focus trap, the header above stays usable, and it stays open across
 * route changes. The scrim closes it, and so does Esc when it's the topmost overlay. Focus moves
 * in on open and back to the opener on close.
 */
const props = withDefaults(
    defineProps<{
        /** A hidden heading. Omit it to label the drawer with a visible heading instead: give that
         * heading the `titleId` the default slot provides. */
        title?: string
        side?: 'left' | 'right'
        /** A px number or any CSS width. */
        width?: number | string
        /** The main navigation: stacks above page drawers (the reader's), below the header. */
        nav?: boolean
    }>(),
    { side: 'left', width: 300 }
)

const open = defineModel<boolean>('open', { required: true })

const titleId = useId()
const panel = useTemplateRef('panel')
const layer = useOverlayLayer(props.nav ? 'nav-drawer' : 'drawer', open)
let opener: HTMLElement | null = null

watch(
    open,
    async isOpen => {
        if (!isOpen) return restoreFocus()
        const active = document.activeElement
        opener = active instanceof HTMLElement && active !== document.body ? active : null
        await nextTick()
        panel.value?.focus({ preventScroll: true })
    },
    { immediate: true }
)

/** Only when focus would otherwise be stranded (in the leaving panel, or lost to `body`). */
function restoreFocus() {
    const target = opener
    opener = null
    const active = document.activeElement
    const inPanel = active instanceof HTMLElement && !!panel.value?.contains(active)
    if (!inPanel && active && active !== document.body) return
    if (target?.isConnected) target.focus({ preventScroll: true })
    else if (inPanel) (active as HTMLElement).blur()
}

useEventListener(document, 'keydown', (e: KeyboardEvent) => {
    if (e.key !== 'Escape' || e.defaultPrevented || !open.value || !layer.isTop()) return
    e.preventDefault()
    open.value = false
})
</script>

<style scoped>
@layer ui {
    .a-drawer-scrim {
        position: fixed;
        inset: 0;
        z-index: var(--z-drawer-scrim);
        background: oklch(0 0 0 / 0.2);
        user-select: none;
        -webkit-tap-highlight-color: transparent;
    }

    .a-drawer {
        position: fixed;
        top: 0;
        bottom: 0;
        z-index: var(--z-drawer);
        display: flex;
        flex-direction: column;
        width: min(var(--drawer-width), 100vw);
        height: 100dvh;
        /* The header overlaps the drawer's top. */
        padding-top: var(--header-height);
        padding-bottom: env(safe-area-inset-bottom);
        overflow-y: auto;
        overscroll-behavior: contain;
        background: var(--color-sidebar);
        color: var(--color-fg);
        box-shadow: var(--shadow-overlay);
        outline: none;
    }

    .a-drawer-scrim.nav {
        z-index: var(--z-sidebar-scrim);
    }

    .a-drawer.nav {
        z-index: var(--z-sidebar);
    }

    .side-left {
        left: 0;
        padding-left: env(safe-area-inset-left);
    }

    .side-right {
        right: 0;
        padding-right: env(safe-area-inset-right);
    }

    .a-drawer-enter-active,
    .a-drawer-leave-active {
        transition: transform var(--duration-medium) var(--ease-standard);
    }

    .side-left:is(.a-drawer-enter-from, .a-drawer-leave-to) {
        transform: translateX(-100%);
    }

    .side-right:is(.a-drawer-enter-from, .a-drawer-leave-to) {
        transform: translateX(100%);
    }

    .a-drawer-scrim-enter-active,
    .a-drawer-scrim-leave-active {
        transition: opacity var(--duration-medium) var(--ease-standard);
    }

    .a-drawer-scrim-enter-from,
    .a-drawer-scrim-leave-to {
        opacity: 0;
    }

    /* A tap during the fade-out reaches the page. */
    .a-drawer-scrim-leave-active {
        pointer-events: none;
    }
}
</style>
