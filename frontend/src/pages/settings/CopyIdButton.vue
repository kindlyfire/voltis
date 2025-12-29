<template>
    <VBtn
        :icon="copied ? 'mdi-check' : 'mdi-content-copy'"
        variant="text"
        size="small"
        :color="copied ? 'success' : undefined"
        :aria-label="copied ? 'Copied library ID' : 'Copy library ID'"
        title="Copy library ID"
        @click="copyId"
    />
</template>

<script setup lang="ts">
import { onUnmounted, ref } from 'vue'

const props = defineProps<{
    id: string
}>()

const copied = ref(false)
let resetTimeout: ReturnType<typeof setTimeout> | null = null

async function copyId() {
    try {
        await navigator.clipboard.writeText(props.id)
        copied.value = true

        if (resetTimeout) clearTimeout(resetTimeout)
        resetTimeout = setTimeout(() => {
            copied.value = false
            resetTimeout = null
        }, 1000)
    } catch (error) {
        console.error('Failed to copy library ID', error)
    }
}

onUnmounted(() => {
    if (resetTimeout) {
        clearTimeout(resetTimeout)
    }
})
</script>
