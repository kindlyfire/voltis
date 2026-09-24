<template>
    <div class="flex min-w-0 flex-col gap-2" :class="{ 'col-span-full': wide }">
        <div class="text-fg-muted font-mono text-xs">{{ label }}</div>
        <div class="flex min-w-0 gap-3" :class="stack ? 'flex-col' : 'flex-wrap items-center'">
            <slot />
        </div>
    </div>
</template>

<script setup lang="ts">
/** One labelled state in the gallery. `stack` stretches the content (fields). */
defineProps<{ label: string; wide?: boolean; stack?: boolean }>()
</script>
