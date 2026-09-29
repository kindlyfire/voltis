<template>
    <div class="page-frame">
        <div class="text-fg-muted flex flex-col items-center gap-2 py-16 text-center">
            <h1 class="text-fg font-display text-xl font-semibold">
                {{ forbidden ? 'Admins only' : 'Page not found' }}
            </h1>
            <p class="text-sm">
                {{ forbidden ? 'Ask an admin for access.' : "This page doesn't exist." }}
            </p>
            <AButton class="mt-2" to="/">Go home</AButton>
        </div>
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import AButton from '@/ui/AButton.vue'

const props = defineProps<{ forbidden?: boolean }>()

useHead({ title: () => (props.forbidden ? 'Admins only' : 'Not found') })
</script>
