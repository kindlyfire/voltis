<template>
    <div
        v-if="errors.length || hint"
        :id="id"
        class="a-field-message"
        :class="{
            invalid: errors.length,
            'tone-warning': !errors.length && hintTone === 'warning',
        }"
    >
        <template v-if="errors.length">
            <p v-for="message in errors" :key="message">
                <AIcon v-if="icon" :icon="IconAlertCircle" class="a-field-message__icon" />{{
                    message
                }}
            </p>
        </template>
        <p v-else>{{ hint }}</p>
    </div>
</template>

<script setup lang="ts">
import AIcon from './AIcon.vue'
import { IconAlertCircle } from './icons'

/** The hint, or the errors (which win), below a form control. Internal. */
defineProps<{
    id: string
    errors: string[]
    hint?: string
    /** Marks errors with an icon, for controls whose own error color is not enough to tell. */
    icon?: boolean
    /** Colors the hint, for a caution about the current value. */
    hintTone?: 'warning'
}>()
</script>

<style scoped>
@layer ui {
    .a-field-message {
        font-size: 12px;
        line-height: 16px;
        color: var(--color-fg-muted);
    }

    .invalid {
        color: var(--color-error);
    }

    .tone-warning {
        color: var(--color-warning);
    }

    .a-field-message__icon {
        display: inline-block;
        margin-inline-end: 4px;
        font-size: 14px;
        vertical-align: -3px;
    }
}
</style>
