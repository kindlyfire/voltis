<template>
    <div class="flex flex-col gap-2.5" aria-hidden="true">
        <div class="rounded-cover aspect-2/3 overflow-hidden">
            <ASkeleton class="h-full" />
        </div>
        <div>
            <ASkeleton shape="text" width="70%" class="text-sm" />
            <ASkeleton v-if="subtitle" shape="text" width="45%" class="text-[13px]" />
        </div>
    </div>
</template>

<script setup lang="ts">
import ASkeleton from '@/ui/ASkeleton.vue'

defineProps<{ subtitle?: boolean }>()
</script>
