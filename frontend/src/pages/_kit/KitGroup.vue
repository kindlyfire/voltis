<template>
    <div class="flex flex-col gap-4">
        <div>
            <h3 class="text-lg font-semibold">{{ title }}</h3>
            <p v-if="description" class="text-fg-muted max-w-2xl text-sm">{{ description }}</p>
        </div>
        <div
            class="grid items-start gap-x-6 gap-y-6"
            :style="{ gridTemplateColumns: `repeat(auto-fill, minmax(min(${min}px, 100%), 1fr))` }"
        >
            <slot />
        </div>
    </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ title: string; description?: string; min?: number }>(), { min: 240 })
</script>
