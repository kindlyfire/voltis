<template>
    <ComboboxRoot
        :open="isOpen"
        :model-value="null"
        ignore-filter
        open-on-focus
        :reset-search-term-on-blur="false"
        :reset-search-term-on-select="false"
        class="search"
        @update:open="open => (wantOpen = open)"
        @update:model-value="navigate"
    >
        <ComboboxAnchor class="search__pill">
            <AIcon :icon="IconMagnify" class="search__icon" />
            <ComboboxInput
                ref="input"
                v-model="term"
                class="search__input"
                type="text"
                enterkeyhint="search"
                placeholder="Search…"
                aria-label="Search"
                aria-keyshortcuts="Control+K"
                @keydown.enter.capture="guardIme"
                @keydown.esc="onEscape"
                @blur="onBlur"
            />
            <AIconButton
                v-if="term"
                :icon="IconClose"
                label="Clear search"
                size="sm"
                :tooltip="false"
                @click="clear"
            />
            <kbd v-else class="search__kbd" aria-hidden="true">Ctrl K</kbd>
        </ComboboxAnchor>
        <ComboboxPortal to="#overlays">
            <ComboboxContent
                position="popper"
                align="start"
                class="a-listbox search__results"
                :side-offset="6"
                :collision-padding="8"
            >
                <ComboboxViewport>
                    <ComboboxItem
                        v-for="item in results"
                        :key="item.id"
                        :value="item.id"
                        class="a-option search__option"
                    >
                        <ACover :src="coverUrl(item)" alt="" class="w-10 flex-none" />
                        <span class="flex min-w-0 flex-col">
                            <span class="line-clamp-2">{{ item.meta.title ?? item.title }}</span>
                            <span class="text-fg-muted text-xs font-normal">
                                {{ displayContentType(item.type) }}
                            </span>
                        </span>
                    </ComboboxItem>
                </ComboboxViewport>
                <div v-if="!results.length" class="search__status">{{ status }}</div>
            </ComboboxContent>
        </ComboboxPortal>
        <div class="sr-only" aria-live="polite">{{ isOpen ? announcement : '' }}</div>
    </ComboboxRoot>
</template>

<script setup lang="ts">
import { refDebounced, useEventListener } from '@vueuse/core'
import {
    ComboboxAnchor,
    ComboboxContent,
    ComboboxInput,
    ComboboxItem,
    ComboboxPortal,
    ComboboxRoot,
    ComboboxViewport,
} from 'reka-ui'
import { computed, ref, useTemplateRef } from 'vue'
import { useRouter } from 'vue-router'
import ACover from '@/ui/ACover.vue'
import AIcon from '@/ui/AIcon.vue'
import AIconButton from '@/ui/AIconButton.vue'
import { IconClose, IconMagnify } from '@/ui/icons'
import { useOverlayLayer } from '@/ui/overlay'
import { contentApi, coverUrl } from '@/utils/api/content'
import { displayContentType } from '@/utils/misc'

const router = useRouter()
const input = useTemplateRef<{ $el: HTMLInputElement }>('input')
const term = ref('')
const debounced = refDebounced(
    computed(() => term.value.trim()),
    300
)
const wantOpen = ref(false)
const isOpen = computed(() => wantOpen.value && !!debounced.value)
useOverlayLayer('menu', isOpen)

// Keyed by the search term, so a slow response for an older term never shows under a newer one.
const query = contentApi.useList(() =>
    debounced.value ? { search: debounced.value, limit: 10, parent_id: 'null' } : undefined
)
const results = computed(() => query.data.value?.data ?? [])

const status = computed(() => {
    if (query.isError.value) return 'Search failed'
    if (query.isFetching.value && !query.data.value) return 'Searching…'
    return 'No results'
})
const announcement = computed(() =>
    results.value.length
        ? `${results.value.length} result${results.value.length === 1 ? '' : 's'}`
        : status.value
)

useEventListener(window, 'keydown', (e: KeyboardEvent) => {
    if (!e.ctrlKey || e.altKey || e.metaKey || e.key.toLowerCase() !== 'k') return
    e.preventDefault()
    input.value?.$el.focus()
    input.value?.$el.select()
})

/** Enter confirming an IME composition must not pick a result (Safari ends it before keydown). */
function guardIme(e: KeyboardEvent) {
    if (e.isComposing || e.keyCode === 229) e.stopImmediatePropagation()
}

/** With the results closed, Esc clears the term (and stays away from a drawer around it). */
function onEscape(e: KeyboardEvent) {
    if (isOpen.value || !term.value) return
    e.preventDefault()
    term.value = ''
}

// Reka closes an open popup on blur itself; one still waiting on the debounce must not open later.
function onBlur() {
    if (!isOpen.value) wantOpen.value = false
}

function clear() {
    term.value = ''
    input.value?.$el.focus()
}

function navigate(id: unknown) {
    if (typeof id !== 'string') return
    wantOpen.value = false
    term.value = ''
    router.push(`/${id}`)
}
</script>

<style scoped>
@layer ui {
    .search {
        min-width: 0;
    }

    .search__pill {
        display: flex;
        align-items: center;
        gap: 12px;
        height: 46px;
        padding: 0 8px 0 16px;
        border-radius: 999px;
        background: var(--color-search);
        color: var(--color-fg);
        cursor: text;
        transition: box-shadow var(--duration-short) var(--ease-standard);

        &:focus-within {
            box-shadow: inset 0 0 0 1.5px var(--color-primary);
        }
    }

    .search__icon {
        font-size: 22px;
        color: var(--color-fg-muted);
    }

    .search__input {
        flex: 1;
        min-width: 0;
        height: 100%;
        border: 0;
        background: none;
        color: inherit;
        font-size: 15px;
        outline: none;

        &::placeholder {
            color: var(--color-fg-muted);
            opacity: 1;
        }
    }

    .search__kbd {
        padding: 3px 9px;
        border-radius: 999px;
        background: var(--color-surface-4);
        color: var(--color-fg-muted);
        font:
            500 12px ui-monospace,
            monospace;
        white-space: nowrap;
    }

    @media (width < 60rem) {
        .search__pill {
            gap: 8px;
            height: 40px;
            padding-left: 12px;
        }

        .search__kbd {
            display: none;
        }
    }
}
</style>

<style>
@layer ui {
    .search__results {
        width: max(300px, var(--reka-combobox-trigger-width));
        max-width: calc(100vw - 16px);
    }

    .search__option {
        gap: 12px;
        padding-block: 6px;
        font-weight: 500;
    }

    .search__status {
        padding: 12px 14px;
        color: var(--color-fg-muted);
        font-size: 14px;
    }
}
</style>
