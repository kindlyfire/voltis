<template>
    <th
        :aria-sort="sort ? (sort === 'asc' ? 'ascending' : 'descending') : undefined"
        class="a-sort-header"
    >
        <button type="button" class="a-sort-header__button a-focus" @click="emit('toggle')">
            <slot />
            <AIcon
                :icon="sort === 'desc' ? IconSortDescending : IconSortAscending"
                class="a-sort-header__icon"
                :class="{ sorted: sort }"
            />
        </button>
    </th>
</template>

<script setup lang="ts">
import AIcon from './AIcon.vue'
import { IconSortAscending, IconSortDescending } from './icons'

/** A sortable column header for ATable. The caller decides what `toggle` does. */
defineProps<{ sort?: 'asc' | 'desc' | null }>()

const emit = defineEmits<{ toggle: [] }>()
</script>

<style scoped>
@layer ui {
    .a-sort-header__button {
        display: inline-flex;
        align-items: center;
        gap: 4px;
        margin-inline: -6px;
        padding: 4px 6px;
        border: 0;
        border-radius: 6px;
        background: none;
        color: inherit;
        font: inherit;
        cursor: pointer;

        &:hover {
            color: var(--color-fg);
        }
    }

    .a-sort-header__icon {
        font-size: 16px;
        opacity: 0;
        transition: opacity var(--duration-short) var(--ease-standard);

        &.sorted {
            color: var(--color-fg);
            opacity: 1;
        }
    }

    .a-sort-header__button:is(:hover, :focus-visible) .a-sort-header__icon:not(.sorted) {
        opacity: 0.5;
    }
}
</style>
