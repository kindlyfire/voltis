<template>
    <div class="a-nav-label"><slot /></div>
</template>

<style scoped>
@layer ui {
    .a-nav-label {
        padding: 14px 16px 6px;
        color: var(--color-fg-muted);
        font-size: 12px;
        font-weight: 600;
        letter-spacing: 0.04em;
    }
}
</style>
