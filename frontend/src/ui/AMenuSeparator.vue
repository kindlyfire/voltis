<template>
    <DropdownMenuSeparator class="a-menu-separator" />
</template>

<script setup lang="ts">
import { DropdownMenuSeparator } from 'reka-ui'
</script>

<style>
@layer ui {
    .a-menu-separator {
        height: 1px;
        margin: 6px 0;
        background: var(--color-outline-variant);
    }
}
</style>
