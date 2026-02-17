<template>
    <VDialog :model-value="open" @update:model-value="v => !v && close(false)" max-width="400">
        <VCard>
            <VCardTitle>{{ title }}</VCardTitle>
            <VCardText>{{ message }}</VCardText>
            <VCardActions>
                <VSpacer />
                <VBtn variant="text" @click="close(false)">Cancel</VBtn>
                <VBtn :color="confirmColor ?? 'primary'" @click="close(true)">
                    {{ confirmText ?? 'Confirm' }}
                </VBtn>
            </VCardActions>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
defineProps<{
    open: boolean
    close: (confirmed: boolean) => void
    title: string
    message: string
    confirmText?: string
    confirmColor?: string
}>()
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './AConfirmModal.vue'

export function showConfirmModal(props: {
    title: string
    message: string
    confirmText?: string
    confirmColor?: string
}): Promise<boolean> {
    return Modals.show<boolean>(Self, props)
}
</script>
