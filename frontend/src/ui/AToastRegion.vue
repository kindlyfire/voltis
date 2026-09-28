<template>
    <ToastProvider label="Notification" swipe-direction="right">
        <ToastRoot
            v-for="toast in visibleToasts"
            :key="toast.id"
            class="a-toast"
            :data-toast="toast.id"
            :open="toast.open"
            :type="toast.tone === 'danger' ? 'foreground' : 'background'"
            :duration="toast.duration ?? (toast.action ? 8000 : 4000)"
            @update:open="open => open || onRekaClose(toast.id)"
            @escape-key-down="(e: KeyboardEvent) => onEscape(e, toast.id)"
        >
            <AIcon :icon="ICONS[toast.tone ?? 'success']" class="a-toast__icon" />
            <ToastDescription class="a-toast__message">{{ toast.message }}</ToastDescription>
            <!-- Not ToastAction/ToastClose: Reka's keyboard close clears the shared pause flag while
            the other toasts stay paused, and they never expire after that. -->
            <button
                v-if="toast.action"
                type="button"
                class="a-toast__action a-state a-focus"
                data-reka-toast-announce-exclude
                :data-reka-toast-announce-alt="toast.action.altText"
                @click="(toast.action.onClick(), close(toast.id))"
            >
                {{ toast.action.label }}
            </button>
            <button
                type="button"
                class="a-toast__close a-state a-focus"
                aria-label="Dismiss"
                @click="close(toast.id)"
            >
                <AIcon :icon="IconClose" />
            </button>
        </ToastRoot>
        <!-- Wraps Reka's focus proxies too, and listens to `focus` in the capture phase: Tab lands on a
        proxy first, which moves focus on in its own focus handler, before any focusin. -->
        <div ref="wrapper" class="contents" @focus.capture="onFocus">
            <ToastViewport class="a-toast-viewport" />
        </div>
    </ToastProvider>
</template>

<script setup lang="ts">
import { ToastDescription, ToastProvider, ToastRoot, ToastViewport } from 'reka-ui'
import { useTemplateRef } from 'vue'
import AIcon from './AIcon.vue'
import { IconAlertCircle, IconCheckCircle, IconClose, IconInformation } from './icons'
import { dismissToast, visibleToasts } from './useToast'

/** Shows `useToast()` toasts, a few at a time. Mount once. F8 focuses the region. */

const ICONS = { success: IconCheckCircle, danger: IconAlertCircle, info: IconInformation }

// Focus that entered the region (F8, Tab) moves to another open toast when its toast closes, and
// goes back where it came from with the last one, instead of staying on the empty region.
const wrapper = useTemplateRef('wrapper')
let returnTo: HTMLElement | null = null
function onFocus(e: FocusEvent) {
    const from = e.relatedTarget
    if (from instanceof HTMLElement && !wrapper.value?.contains(from)) returnTo = from
    // F8 focuses the list itself: go on to the newest toast, so that Esc closes it.
    const target = e.target as HTMLElement
    if (target.classList.contains('a-toast-viewport')) {
        const newest = visibleToasts.value.findLast(t => t.open)
        if (newest) target.querySelector<HTMLElement>(`[data-toast="${newest.id}"]`)?.focus()
    }
}

// Reka closes every toast on any Escape in the page, so Esc in a popup, dialog or drawer would
// dismiss them all. Only the focused toast closes. (Reka's popups and dialogs listen on window
// after the toasts, so preventing the event here would keep those open instead.)
let keepOnEscape: number | null = null
function onEscape(e: KeyboardEvent, id: number) {
    const target = e.target instanceof Element ? e.target : null
    if (
        !e.defaultPrevented &&
        target?.closest('.a-toast')?.getAttribute('data-toast') !== String(id)
    )
        keepOnEscape = id
}
function onRekaClose(id: number) {
    if (keepOnEscape === id) keepOnEscape = null
    else close(id)
}

function close(id: number) {
    const region = wrapper.value
    const active = document.activeElement
    if (region?.contains(active)) {
        // The next one in Tab order (newest first), else the one before it.
        const list = visibleToasts.value
        const at = list.findIndex(t => t.id === id)
        const next = list.slice(0, at).findLast(t => t.open) ?? list.slice(at + 1).find(t => t.open)
        const nextEl = next && region.querySelector<HTMLElement>(`[data-toast="${next.id}"]`)
        if (nextEl) {
            // Focus was in the closing toast, or on the region (F8, or Reka's Escape close).
            const toast = active!.closest('.a-toast')
            if (!toast || toast.getAttribute('data-toast') === String(id)) nextEl.focus()
        } else {
            if (returnTo?.isConnected) returnTo.focus()
            returnTo = null
        }
    }
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
        /* A modal dialog sets `pointer-events: none` on body. Reka's inline `none` still wins while
         * the region is empty. */
        pointer-events: auto;
        display: flex;
        flex-direction: column;
        gap: 8px;
        width: max-content;
        max-width: calc(100vw - 32px);
        margin: 0;
        padding: 0;
        list-style: none;
        transform: translateX(-50%);
        outline: none;

        @media (width < 40rem) {
            right: 16px;
            left: 16px;
            bottom: calc(16px + env(safe-area-inset-bottom));
            width: auto;
            max-width: none;
            transform: none;
        }
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
        padding-inline-end: 6px;
        box-shadow: 0 10px 30px -10px oklch(0.3 0.03 60 / 0.35);

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
