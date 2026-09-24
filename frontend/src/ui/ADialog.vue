<template>
    <DialogRoot :open="open" @update:open="setOpen">
        <DialogPortal to="#overlays">
            <DialogOverlay class="a-dialog-scrim" />
            <DialogContent
                ref="dialogContent"
                class="a-dialog"
                :class="[`a-dialog--${size}`, `a-dialog--${variant}`]"
                v-bind="description ? {} : { 'aria-describedby': undefined }"
                @open-auto-focus="onOpenAutoFocus"
                @close-auto-focus="onCloseAutoFocus"
                @interact-outside="!dismissible && $event.preventDefault()"
            >
                <div v-if="variant === 'default'" class="a-dialog__header">
                    <DialogTitle class="a-dialog__title">
                        {{ title }}
                    </DialogTitle>
                    <div v-if="$slots.headerActions" class="a-dialog__header-actions">
                        <slot name="headerActions" />
                    </div>
                </div>
                <DialogTitle v-else class="sr-only">
                    {{ title }}
                </DialogTitle>
                <DialogDescription v-if="description" class="a-dialog__description">
                    {{ description }}
                </DialogDescription>

                <div v-if="$slots.default" class="a-dialog__body">
                    <slot />
                </div>

                <div v-if="$slots.actions" class="a-dialog__actions">
                    <slot name="actions" />
                </div>

                <DialogClose v-if="variant === 'bare'" as-child>
                    <AIconButton
                        class="a-dialog__close"
                        :icon="IconClose"
                        label="Close"
                        variant="tonal"
                        :tooltip="false"
                    />
                </DialogClose>
            </DialogContent>
        </DialogPortal>
    </DialogRoot>
</template>

<script lang="ts">
import type { InjectionKey } from 'vue'

const parentDialogKey: InjectionKey<() => HTMLElement | null> = Symbol('parentDialog')
</script>

<script setup lang="ts">
import {
    DialogClose,
    DialogContent,
    DialogDescription,
    DialogOverlay,
    DialogPortal,
    DialogRoot,
    DialogTitle,
} from 'reka-ui'
import { inject, provide, useTemplateRef, watch } from 'vue'
import { modalEntryKey } from '@/utils/modals'
import AIconButton from './AIconButton.vue'
import { IconClose } from './icons'
import { focusOwnerKey, useOverlayLayer } from './overlay'

const props = withDefaults(
    defineProps<{
        title: string
        description?: string
        size?: 'sm' | 'md' | 'lg' | 'xl'
        /** Esc and outside clicks close the dialog. */
        dismissible?: boolean
        /** `bare` has no chrome (image lightbox) but keeps a hidden title and a close button. */
        variant?: 'default' | 'bare'
    }>(),
    { size: 'md', dismissible: true, variant: 'default' }
)

const open = defineModel<boolean>('open', { required: true })

// When rendered by `Modals.show`, the entry ends after the leave transition (focus return and
// cleanup). Nested dialogs must not act for it.
const entry = inject(modalEntryKey, null)
provide(modalEntryKey, null)
provide(focusOwnerKey, true)

useOverlayLayer('dialog', open)

const contentRef = useTemplateRef<{ $el: HTMLElement }>('dialogContent')
const content = () => contentRef.value?.$el ?? null
const parentContent = inject(parentDialogKey, null)
provide(parentDialogKey, content)

// Reka returns focus to a DialogTrigger, which we don't render: remember the opener ourselves.
let opener: Element | null = null
watch(
    open,
    isOpen => {
        if (isOpen) opener = document.activeElement
    },
    { immediate: true }
)

function setOpen(value: boolean) {
    if (!value && !props.dismissible) return
    open.value = value
}

// First `[autofocus]` element, else the first field, else the dialog itself.
function onOpenAutoFocus(e: Event) {
    const el = content()
    if (!el) return
    const target =
        el.querySelector<HTMLElement>('[autofocus]') ??
        el.querySelector<HTMLElement>('input:not([type=hidden]), textarea, select')
    e.preventDefault()
    ;(target ?? el).focus({ preventScroll: true })
}

// Focus goes back to the opener. A dialog nested in another falls back to that one when its opener
// is gone (the nested dialog's action removed it), unless the caller already placed focus in it.
function onCloseAutoFocus(e: Event) {
    e.preventDefault()
    if (entry) return entry.afterLeave()
    const parent = parentContent?.()
    const active = document.activeElement
    if (parent && active !== parent && parent.contains(active)) return
    const target = opener instanceof HTMLElement && opener.isConnected ? opener : parent
    target?.focus({ preventScroll: true })
}
</script>

<!-- Unscoped: Reka renders these parts outside this component's scope. -->
<style>
@layer ui {
    .a-dialog-scrim {
        position: fixed;
        inset: 0;
        z-index: var(--z-dialog);
        background: oklch(0.15 0.01 75 / 0.45);

        &[data-state='open'] {
            animation: a-fade-in var(--duration-medium) var(--ease-standard);
        }

        &[data-state='closed'] {
            animation: a-fade-out var(--duration-short) var(--ease-standard);
        }
    }

    .a-dialog {
        position: fixed;
        top: 50%;
        left: 50%;
        z-index: var(--z-dialog);
        display: flex;
        flex-direction: column;
        width: min(var(--dialog-width), calc(100vw - 32px));
        max-height: calc(100dvh - 32px);
        border-radius: var(--radius-dialog);
        background: var(--color-raised);
        color: var(--color-fg);
        box-shadow: var(--shadow-overlay);
        transform: translate(-50%, -50%);
        outline: none;

        &[data-state='open'] {
            animation: a-dialog-in var(--duration-medium) var(--ease-standard);
        }

        &[data-state='closed'] {
            animation: a-dialog-out var(--duration-short) var(--ease-standard);
        }
    }

    .a-dialog--sm {
        --dialog-width: 400px;
    }

    .a-dialog--md {
        --dialog-width: 500px;
    }

    .a-dialog--lg {
        --dialog-width: 700px;
    }

    .a-dialog--xl {
        --dialog-width: 1000px;
    }

    .a-dialog__header {
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 24px 24px 0;
    }

    .a-dialog__title {
        flex: 1;
        font-family: var(--font-display);
        font-size: 22px;
        font-weight: 600;
        line-height: 1.25;
    }

    .a-dialog__header-actions {
        display: flex;
        gap: 4px;
        margin: -6px -8px -6px 0;
    }

    .a-dialog__description {
        padding: 14px 24px 0;
        color: var(--color-fg-muted);
        font-size: 14px;
        line-height: 1.5;
    }

    .a-dialog__body {
        flex: 1;
        min-height: 0;
        overflow-y: auto;
        /* Bottom padding inside the scroll container, so focus rings and shadows on the last
           element aren't clipped; the actions' top padding makes up the rest of the gap. */
        padding: 14px 24px 12px;
        scroll-padding-block: 12px;
        font-size: 14px;
        line-height: 1.5;
        overscroll-behavior: contain;
    }

    .a-dialog__actions {
        display: flex;
        flex-wrap: wrap;
        justify-content: flex-end;
        gap: 8px;
        padding: 10px 24px 24px;
    }

    .a-dialog--default > :last-child:not(.a-dialog__actions) {
        padding-bottom: 24px;
    }

    .a-dialog--bare {
        width: auto;
        max-width: calc(100vw - 32px);
        background: transparent;
        box-shadow: none;

        & .a-dialog__body {
            padding: 0;
        }
    }

    .a-dialog__close {
        position: absolute;
        top: 8px;
        right: 8px;
    }

    @keyframes a-fade-in {
        from {
            opacity: 0;
        }
    }

    @keyframes a-fade-out {
        to {
            opacity: 0;
        }
    }

    @keyframes a-dialog-in {
        from {
            opacity: 0;
            transform: translate(-50%, -50%) scale(0.96);
        }
    }

    @keyframes a-dialog-out {
        to {
            opacity: 0;
            transform: translate(-50%, -50%) scale(0.96);
        }
    }
}
</style>
