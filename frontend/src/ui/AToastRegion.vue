<template>
    <ToastProvider label="Notification" swipe-direction="right">
        <ToastRoot
            v-if="current"
            :key="current.id"
            class="a-toast"
            :class="{ 'has-action': current.action }"
            :open="current.open"
            :type="current.tone === 'danger' ? 'foreground' : 'background'"
            :duration="current.duration ?? (current.action ? 8000 : 4000)"
            @update:open="open => open || close(current!.id)"
        >
            <AIcon :icon="ICONS[current.tone ?? 'success']" class="a-toast__icon" />
            <ToastDescription class="a-toast__message">{{ current.message }}</ToastDescription>
            <template v-if="current.action">
                <ToastAction :alt-text="current.action.altText" as-child>
                    <button
                        type="button"
                        class="a-toast__action a-state a-focus"
                        @click="current.action.onClick()"
                    >
                        {{ current.action.label }}
                    </button>
                </ToastAction>
                <ToastClose as-child>
                    <button
                        type="button"
                        class="a-toast__close a-state a-focus"
                        aria-label="Dismiss"
                    >
                        <AIcon :icon="IconClose" />
                    </button>
                </ToastClose>
            </template>
        </ToastRoot>
        <ToastViewport class="a-toast-viewport" @focusin="onFocusin" />
    </ToastProvider>
</template>

<script setup lang="ts">
import {
    ToastAction,
    ToastClose,
    ToastDescription,
    ToastProvider,
    ToastRoot,
    ToastViewport,
} from 'reka-ui'
import { computed } from 'vue'
import AIcon from './AIcon.vue'
import { IconAlertCircle, IconCheckCircle, IconClose, IconInformation } from './icons'
import { dismissToast, toasts } from './useToast'

/** Shows `useToast()` toasts one at a time. Mount once. F8 focuses the region. */
const current = computed(() => toasts.value[0])

const ICONS = { success: IconCheckCircle, danger: IconAlertCircle, info: IconInformation }

// Focus that entered the region (F8, Tab) goes back where it came from when its toast closes,
// instead of staying on the empty region.
let region: HTMLElement | null = null
let returnTo: HTMLElement | null = null
function onFocusin(e: FocusEvent) {
    region = e.currentTarget as HTMLElement
    const from = e.relatedTarget
    if (from instanceof HTMLElement && !region.contains(from)) returnTo = from
}

function close(id: number) {
    if (region?.contains(document.activeElement) && returnTo?.isConnected) returnTo.focus()
    returnTo = null
    dismissToast(id)
}
</script>

<style>
@layer ui {
    .a-toast-viewport {
        position: fixed;
        bottom: calc(28px + env(safe-area-inset-bottom));
        left: 50%;
        z-index: var(--z-toast);
        display: flex;
        flex-direction: column;
        width: max-content;
        max-width: calc(100vw - 32px);
        margin: 0;
        padding: 0;
        list-style: none;
        transform: translateX(-50%);
        outline: none;
    }

    .a-toast {
        display: flex;
        align-items: center;
        gap: 10px;
        min-height: 48px;
        padding: 6px 16px;
        border-radius: var(--radius-toast);
        background: var(--color-inverse);
        color: var(--color-on-inverse);
        font-size: 14px;
        line-height: 1.4;
        box-shadow: 0 10px 30px -10px oklch(0.3 0.03 60 / 0.35);

        &.has-action {
            padding-inline-end: 6px;
        }

        &[data-state='open'] {
            animation: a-toast-in var(--duration-medium) var(--ease-standard);
        }

        &[data-state='closed'] {
            animation: a-toast-out var(--duration-short) var(--ease-standard);
        }

        &[data-swipe='move'] {
            transform: translateX(var(--reka-toast-swipe-move-x));
        }

        &[data-swipe='cancel'] {
            transform: translateX(0);
            transition: transform var(--duration-short) var(--ease-standard);
        }

        &[data-swipe='end'] {
            animation: a-toast-swipe-out var(--duration-short) var(--ease-standard);
        }
    }

    /* The primary ring is invisible on the inverse surface. */
    .a-toast .a-focus:focus-visible {
        outline-color: var(--color-inverse-primary);
    }

    .a-toast:focus-visible {
        outline: 2px solid var(--color-inverse-primary);
        outline-offset: -4px;
    }

    .a-toast__icon {
        font-size: 20px;
        color: var(--color-inverse-primary);
    }

    .a-toast__message {
        flex: 1;
        min-width: 0;
    }

    .a-toast__action,
    .a-toast__close {
        flex: none;
        height: 36px;
        border: 0;
        border-radius: 999px;
        background: none;
        cursor: pointer;
    }

    .a-toast__action {
        padding: 0 12px;
        color: var(--color-inverse-primary);
        font-weight: 600;
    }

    .a-toast__close {
        display: grid;
        place-items: center;
        width: 36px;
        color: inherit;
        font-size: 20px;
    }

    @keyframes a-toast-in {
        from {
            opacity: 0;
            transform: translateY(12px);
        }
    }

    @keyframes a-toast-out {
        to {
            opacity: 0;
        }
    }

    @keyframes a-toast-swipe-out {
        from {
            transform: translateX(var(--reka-toast-swipe-end-x));
        }

        to {
            opacity: 0;
            transform: translateX(100%);
        }
    }
}
</style>
