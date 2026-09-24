<template>
    <AIconButton
        :icon="IconContentCopy"
        :label="`Copy the ID of ${name}`"
        size="sm"
        @click="copyId"
    />
</template>

<script setup lang="ts">
import AIconButton from '@/ui/AIconButton.vue'
import { IconContentCopy } from '@/ui/icons'
import { useToast } from '@/ui/useToast'

const props = defineProps<{
    id: string
    name: string
}>()

const toast = useToast()

async function copyId() {
    try {
        await navigator.clipboard.writeText(props.id)
        toast.show({ message: `Copied the ID of ${props.name}`, tone: 'info' })
    } catch {
        toast.show({ message: 'Could not copy the ID', tone: 'danger' })
    }
}
</script>
