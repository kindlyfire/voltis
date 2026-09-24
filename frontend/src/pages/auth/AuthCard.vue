<template>
    <main class="flex min-h-dvh flex-col items-center justify-center gap-6 px-4 py-10">
        <p class="font-display text-[23px] font-semibold" aria-hidden="true">Voltis</p>
        <ACard class="w-full max-w-[420px]" :aria-busy="loading || undefined">
            <h1 class="font-display text-[28px] leading-tight font-semibold">{{ title }}</h1>
            <div v-if="loading" class="flex justify-center py-8">
                <ASpinner size="lg" />
            </div>
            <slot v-else />
            <template v-if="$slots.footer" #actions>
                <slot name="footer" />
            </template>
        </ACard>
    </main>
</template>

<script setup lang="ts">
import ACard from '@/ui/ACard.vue'
import ASpinner from '@/ui/ASpinner.vue'

/** The centered card the sign-in pages share. */
defineProps<{ title: string; loading?: boolean }>()
</script>
