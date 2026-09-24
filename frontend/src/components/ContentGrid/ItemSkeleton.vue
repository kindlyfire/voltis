<template>
    <div class="flex flex-col gap-2.5" aria-hidden="true">
        <div class="rounded-cover aspect-2/3 overflow-hidden">
            <ASkeleton class="h-full" />
        </div>
        <ASkeleton shape="text" width="70%" class="text-sm" />
    </div>
</template>

<script setup lang="ts">
import ASkeleton from '@/ui/ASkeleton.vue'
</script>
