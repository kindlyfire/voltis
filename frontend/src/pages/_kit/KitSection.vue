<template>
    <section :id="id" class="flex scroll-mt-20 flex-col gap-8">
        <header class="border-outline-variant flex flex-col gap-1 border-b pb-3">
            <h2 class="font-display text-3xl font-semibold">{{ title }}</h2>
            <p v-if="description" class="text-fg-muted max-w-2xl text-sm">{{ description }}</p>
        </header>
        <slot />
    </section>
</template>

<script setup lang="ts">
defineProps<{ id: string; title: string; description?: string }>()
</script>
