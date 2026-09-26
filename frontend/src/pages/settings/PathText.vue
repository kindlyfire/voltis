<template>
    <!-- rtl puts the ellipsis at the start, keeping the folder's name visible. -->
    <span dir="rtl" class="truncate text-left" :title="path">
        <bdi dir="ltr">{{ path }}</bdi>
    </span>
</template>

<script setup lang="ts">
/** A path truncated at the start, with the full path in its title. */
defineProps<{ path: string }>()
</script>
