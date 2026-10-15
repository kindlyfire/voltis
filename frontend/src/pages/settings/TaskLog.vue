<template>
    <pre
        class="bg-surface-3 rounded-field max-h-100 overflow-auto p-3 text-xs leading-snug wrap-break-word whitespace-pre-wrap"
        >{{ text }}</pre>
</template>

<script setup lang="ts">
defineProps<{ text: string }>()
</script>
