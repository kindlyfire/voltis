<template>
    <ComboboxRoot
        ref="root"
        :open="isOpen"
        :model-value="null"
        ignore-filter
        open-on-focus
        :reset-search-term-on-blur="false"
        :reset-search-term-on-select="false"
        class="min-w-0"
        @update:open="open => (wantOpen = open)"
        @update:model-value="navigate"
    >
        <ComboboxAnchor class="search__pill">
            <ASpinner v-if="loading" size="inherit" decorative class="search__icon" />
            <AIcon v-else :icon="IconMagnify" class="search__icon search__magnifier" />
            <ComboboxInput
                ref="input"
                v-model="term"
                class="search__input"
                type="text"
                enterkeyhint="search"
                placeholder="Search…"
                aria-label="Search"
                aria-keyshortcuts="Control+K"
                @keydown.enter.capture="guardEnter"
                @keydown.up.down.home.end="markNavigated"
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
                :aria-busy="loading || undefined"
                @pointermove.capture="onPointerMove"
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
                    <div v-if="!results.length" class="search__status">{{ status }}</div>
                </ComboboxViewport>
            </ComboboxContent>
        </ComboboxPortal>
        <div class="sr-only" aria-live="polite">
            {{ isOpen ? announcement : '' }}
        </div>
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
import { computed, onMounted, ref, shallowRef, useTemplateRef, watch } from 'vue'
import { useRouter } from 'vue-router'
import ACover from '@/ui/ACover.vue'
import AIcon from '@/ui/AIcon.vue'
import AIconButton from '@/ui/AIconButton.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { IconClose, IconMagnify } from '@/ui/icons'
import { useOverlayLayer } from '@/ui/overlay'
import { contentApi, coverUrl } from '@/utils/api/content'
import type { Content, UncountedPage } from '@/utils/api/types'
import { displayContentType } from '@/utils/misc'

const props = defineProps<{ autofocus?: boolean }>()
/** Esc on an empty term. */
const emit = defineEmits<{ close: [] }>()

const router = useRouter()
const root = useTemplateRef('root')
const input = useTemplateRef<{ $el: HTMLInputElement }>('input')
const term = ref('')
const trimmed = computed(() => term.value.trim())
const debounced = refDebounced(trimmed, 300)
const wantOpen = ref(false)
const isOpen = computed(() => wantOpen.value && !!debounced.value)
useOverlayLayer('menu', isOpen)

// Keyed by the search term, so a late response for an older term can't overwrite a newer one's.
const query = contentApi.useListUncounted(() =>
    debounced.value ? { search: debounced.value, limit: 10, parent_id: 'null' } : undefined
)
const last = shallowRef<UncountedPage<Content>>()
// Set while the term is blank until the debounce catches up, so a late response for the term
// that was cleared doesn't refill `last`.
let blanked = false
let navigated = false
watch(term, () => {
    navigated = false
    if (trimmed.value) return
    last.value = undefined
    blanked = true
})
watch(debounced, () => (blanked = false))
watch([query.data, isOpen, query.isError], ([d, open, err]) => {
    if (err || !open) last.value = undefined
    else if (d && trimmed.value && !blanked) last.value = d
})
const data = computed(() => query.data.value ?? last.value)

function markNavigated(e: KeyboardEvent) {
    // Reka ignores navigation keys mid-composition (see guardEnter for Safari).
    if (!e.isComposing && e.keyCode !== 229) navigated = true
}
function onPointerMove(e: PointerEvent) {
    const option = (e.target as Element).closest('[role="option"]')
    // Reka's root pointerleave fires when moving from the pill into the portaled popup and clears
    // the highlight; the first row is then still the automatic one.
    const auto =
        root.value?.highlightedElement ??
        (e.currentTarget as Element).querySelector('[role="option"]')
    if (option && option !== auto) navigated = true
}
// Reka doesn't reset the highlight when a non-empty list is replaced.
watch(
    query.data,
    d => {
        const el = root.value?.highlightedElement
        if (d?.data.length && (!navigated || !el?.isConnected)) root.value?.highlightFirstItem?.()
    },
    { flush: 'post' }
)
const results = computed(() => data.value?.data ?? [])
// Results on screen don't match the term yet: the debounce is pending or the new term has no data.
// Background refetches of the current term don't count; isPending (unlike isLoading) also covers
// a fetch paused while offline.
const pending = computed(
    () => trimmed.value !== debounced.value || (!!debounced.value && query.isPending.value)
)
// A blank term's pending debounce only closes the popup, so it doesn't spin.
const loading = computed(() => !!trimmed.value && pending.value)

const status = computed(() => {
    if (query.isError.value) return 'Search failed'
    if (query.isFetching.value && !data.value) return 'Searching…'
    return 'No results'
})
const announcement = computed(() => {
    // Old results are still on screen; with none, "Searching…" is announced instead.
    if (pending.value && results.value.length) return ''
    return results.value.length
        ? `${results.value.length} result${results.value.length === 1 ? '' : 's'}`
        : status.value
})

useEventListener(window, 'keydown', (e: KeyboardEvent) => {
    if (!e.ctrlKey || e.altKey || e.metaKey || e.key.toLowerCase() !== 'k') return
    e.preventDefault()
    input.value?.$el.focus()
    input.value?.$el.select()
})

function guardEnter(e: KeyboardEvent) {
    // Safari ends an IME composition before keydown, so also check keyCode 229.
    if (e.isComposing || e.keyCode === 229) return e.stopImmediatePropagation()
    // The highlighted row belongs to an older term.
    if (!pending.value) return
    e.preventDefault()
    e.stopImmediatePropagation()
}

/** With the results closed, Esc clears the term (and stays away from a drawer around it). */
function onEscape(e: KeyboardEvent) {
    if (isOpen.value) return
    if (!term.value) return emit('close')
    e.preventDefault()
    term.value = ''
}

// Reka closes an open popup on blur itself; one still waiting on the debounce must not open later.
function onBlur() {
    if (!isOpen.value) wantOpen.value = false
}

onMounted(() => {
    if (props.autofocus) input.value?.$el.focus()
})

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
    }

    .search__magnifier {
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
