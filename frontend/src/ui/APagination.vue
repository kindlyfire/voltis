<template>
    <nav class="a-pagination" :aria-label="label">
        <AIconButton
            :icon="IconChevronLeft"
            label="Previous page"
            size="sm"
            :disabled="page <= 1"
            focusable-when-disabled
            @click="page--"
        />
        <span class="a-pagination__compact" role="status">Page {{ page }} of {{ length }}</span>
        <template v-for="(item, i) in items" :key="item === 'gap' ? `gap-${i}` : item">
            <span v-if="item === 'gap'" class="a-pagination__gap" aria-hidden="true">…</span>
            <button
                v-else
                type="button"
                class="a-pagination__page a-state a-focus"
                :class="{ current: item === page }"
                :aria-current="item === page ? 'page' : undefined"
                :aria-label="`Page ${item}`"
                @click="page = item"
            >
                {{ item }}
            </button>
        </template>
        <AIconButton
            :icon="IconChevronRight"
            label="Next page"
            size="sm"
            :disabled="page >= length"
            focusable-when-disabled
            @click="page++"
        />
    </nav>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import AIconButton from './AIconButton.vue'
import { IconChevronLeft, IconChevronRight } from './icons'
import { pageItems } from './pagination'

const props = withDefaults(defineProps<{ length: number; label?: string }>(), {
    label: 'Pagination',
})

/** 1-based. Clamped to `length` when that shrinks. */
const page = defineModel<number>('page', { required: true })

const items = computed(() => pageItems(page.value, props.length))

watch(
    () => [page.value, props.length] as const,
    ([current, length]) => {
        if (length > 0 && current > length) page.value = length
        else if (current < 1) page.value = 1
    },
    { immediate: true }
)
</script>

<style scoped>
@layer ui {
    .a-pagination {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        justify-content: center;
        gap: 4px;
    }

    .a-pagination__page {
        min-width: 32px;
        height: 32px;
        padding: 0 6px;
        border: 0;
        border-radius: 999px;
        background: transparent;
        color: var(--color-fg);
        font-size: 14px;
        font-variant-numeric: tabular-nums;
        cursor: pointer;

        &.current {
            background: var(--color-secondary-container);
            color: var(--color-on-secondary-container);
            font-weight: 600;
        }
    }

    .a-pagination__gap {
        min-width: 20px;
        color: var(--color-fg-muted);
        text-align: center;
    }

    .a-pagination__compact {
        display: none;
        padding-inline: 8px;
        font-size: 14px;
        font-variant-numeric: tabular-nums;
    }

    /* Seven page buttons don't fit a phone: show the position with prev/next only. */
    @media (width < 40rem) {
        .a-pagination__compact {
            display: inline;
        }

        .a-pagination__page,
        .a-pagination__gap {
            display: none;
        }
    }
}
</style>
