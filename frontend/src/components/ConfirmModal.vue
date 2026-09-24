<template>
    <ADialog
        :open="open"
        :title="title"
        :description="message"
        size="sm"
        @update:open="v => !v && close(false)"
    >
        <template #actions>
            <AButton variant="text" tone="neutral" @click="close(false)">Cancel</AButton>
            <!-- A danger confirm keeps focus on the dialog, so Enter can't destroy by accident. -->
            <AButton :tone="tone" :autofocus="tone === 'primary'" @click="close(true)">
                {{ confirmText ?? 'Confirm' }}
            </AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'

withDefaults(defineProps<ConfirmProps & { open: boolean; close: (confirmed: boolean) => void }>(), {
    tone: 'primary',
})
</script>

<script lang="ts">
import { Modals, type ShowOptions } from '@/utils/modals'
import Self from './ConfirmModal.vue'

export type ConfirmProps = {
    title: string
    message: string
    confirmText?: string
    tone?: 'primary' | 'danger'
}

export function showConfirmModal(props: ConfirmProps, options?: ShowOptions): Promise<boolean> {
    return Modals.show<boolean>(Self, props, options)
}
</script>
