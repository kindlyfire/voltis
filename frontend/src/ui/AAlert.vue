<template>
    <div class="a-alert" :class="`tone-${tone}`" :role="tone === 'danger' ? 'alert' : 'status'">
        <AIcon :icon="ICONS[tone]" class="a-alert__icon" />
        <div class="a-alert__body">
            <p v-if="title" class="a-alert__title">{{ title }}</p>
            <slot />
        </div>
        <span v-if="dismissible" class="a-alert__dismiss">
            <AIconButton
                :icon="IconClose"
                label="Dismiss"
                size="sm"
                :tooltip="false"
                @click="emit('dismiss')"
            />
        </span>
    </div>
</template>

<script setup lang="ts">
import AIcon from './AIcon.vue'
import AIconButton from './AIconButton.vue'
import { IconAlert, IconAlertCircle, IconCheckCircle, IconClose, IconInformation } from './icons'

withDefaults(
    defineProps<{
        tone?: 'info' | 'success' | 'warning' | 'danger'
        title?: string
        dismissible?: boolean
    }>(),
    { tone: 'info' }
)

const emit = defineEmits<{ dismiss: [] }>()

const ICONS = {
    info: IconInformation,
    success: IconCheckCircle,
    warning: IconAlert,
    danger: IconAlertCircle,
}
</script>

<style scoped>
@layer ui {
    .a-alert {
        display: flex;
        align-items: flex-start;
        gap: 12px;
        padding: 12px 16px;
        border-radius: var(--radius-field);
        background: var(--alert-bg);
        color: var(--alert-fg);
        font-size: 14px;
        line-height: 1.5;
    }

    .tone-info {
        --alert-bg: var(--color-info-container);
        --alert-fg: var(--color-on-info-container);
    }

    .tone-success {
        --alert-bg: var(--color-success-container);
        --alert-fg: var(--color-on-success-container);
    }

    .tone-warning {
        --alert-bg: var(--color-warning-container);
        --alert-fg: var(--color-on-warning-container);
    }

    .tone-danger {
        --alert-bg: var(--color-error-container);
        --alert-fg: var(--color-on-error-container);
    }

    .a-alert__icon {
        margin-top: 1px;
        font-size: 20px;
    }

    .a-alert__body {
        flex: 1;
        min-width: 0;
        overflow-wrap: anywhere;
    }

    .a-alert__title {
        font-weight: 600;
    }

    .a-alert__dismiss {
        display: flex;
        margin: -6px -8px -6px 0;

        & :deep(.a-icon-button) {
            --btn-fg: currentColor;
        }
    }
}
</style>
