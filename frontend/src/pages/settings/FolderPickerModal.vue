<template>
    <ADialog
        :open="open"
        :title="multiple ? 'Choose folders' : 'Choose a folder'"
        size="lg"
        @update:open="v => !v && close(null)"
    >
        <div class="flex flex-col gap-3">
            <div class="flex items-center gap-1">
                <AIconButton :icon="IconHome" label="Home" size="sm" @click="goHome" />
                <AIconButton
                    :icon="IconArrowUp"
                    label="Up"
                    size="sm"
                    :disabled="!parent"
                    @click="goUp"
                />
                <form v-if="editing" class="min-w-0 flex-1" @submit.prevent="submitPath">
                    <ATextField
                        ref="pathInput"
                        v-model="draft"
                        label="Path"
                        size="sm"
                        autocomplete="off"
                        spellcheck="false"
                        @keydown.esc="stopEditing"
                    />
                </form>
                <div
                    v-else
                    class="bg-field rounded-field flex h-10 min-w-0 flex-1 items-center pl-1"
                    @click.self="edit"
                >
                    <nav
                        ref="crumbNav"
                        aria-label="Path"
                        class="flex min-w-0 items-center overflow-x-auto"
                    >
                        <template v-for="(crumb, i) in crumbs" :key="crumb.path">
                            <AIcon
                                v-if="i > 1"
                                :icon="IconChevronRight"
                                class="text-fg-muted shrink-0"
                            />
                            <button
                                type="button"
                                class="a-state a-focus shrink-0 rounded-md px-1.5 py-0.5 whitespace-nowrap"
                                :aria-current="i === crumbs.length - 1 ? 'location' : undefined"
                                @click="navigate(crumb.path)"
                            >
                                {{ crumb.name }}
                            </button>
                        </template>
                        <span v-if="!crumbs.length" class="text-fg-muted px-1.5">
                            {{ requested ?? 'Home' }}
                        </span>
                    </nav>
                    <AIconButton
                        :icon="IconPencil"
                        label="Type a path"
                        size="sm"
                        class="ml-auto shrink-0"
                        @click="edit"
                    />
                </div>
                <AIconButton :icon="IconRefresh" label="Refresh" size="sm" @click="refresh" />
            </div>
            <ASwitch v-if="requested !== null" v-model="hidden" label="Show hidden folders" />

            <AAlert v-if="notice">{{ notice }}</AAlert>
            <AAlert v-if="resolvedTo && !loading">Resolved to {{ resolvedTo }}</AAlert>
            <AAlert v-if="error" tone="danger">{{ error }}</AAlert>
            <AAlert v-if="displayed?.truncated && !loading" tone="warning">
                Some folders are omitted; type a path to go directly.
            </AAlert>
            <AAlert v-if="currentWarning" tone="warning">{{ currentWarning.long }}</AAlert>

            <div
                ref="listEl"
                class="border-outline-variant h-[min(360px,45dvh)] overflow-y-auto rounded-xl border p-1 outline-none"
                tabindex="-1"
                autofocus
                @keydown="onListKeydown"
            >
                <div v-if="loading" class="flex h-full items-center justify-center">
                    <ASpinner label="Loading folders" />
                </div>
                <div v-else-if="requested === null" class="flex flex-col gap-3 p-2">
                    <p class="text-fg-muted text-sm">
                        Folders available to the server. In Docker, use the mounted container path.
                    </p>
                    <section class="flex flex-col">
                        <h3 class="mb-1 px-2 text-sm font-semibold">Mounts</h3>
                        <AAlert v-if="roots.data.value?.mounts_error" tone="warning">
                            Could not read the mount points: {{ roots.data.value.mounts_error }}
                        </AAlert>
                        <QueryError :query="roots" />
                        <button
                            v-for="mount in roots.data.value?.mounts"
                            :key="mount.path"
                            data-row
                            type="button"
                            :class="rowClass"
                            @click="enter(mount.path)"
                        >
                            <AIcon :icon="IconHardDrive" class="shrink-0 text-lg" />
                            <span class="truncate">{{ mount.path }}</span>
                            <span class="text-fg-muted shrink-0 text-xs">
                                {{ mount.timed_out ? 'Not responding' : mount.fstype }}
                            </span>
                        </button>
                    </section>
                    <section v-if="overlaps.allSources.value.length" class="flex flex-col">
                        <h3 class="mb-1 px-2 text-sm font-semibold">Library sources</h3>
                        <button
                            v-for="source in overlaps.allSources.value"
                            :key="`${source.label}:${source.path}`"
                            data-row
                            type="button"
                            :class="rowClass"
                            @click="enter(source.path, true)"
                        >
                            <AIcon :icon="IconFolder" class="shrink-0 text-lg" />
                            <span class="truncate">{{ source.path }}</span>
                            <span class="text-fg-muted shrink-0 text-xs">{{ source.label }}</span>
                        </button>
                    </section>
                </div>
                <p v-else-if="displayed && !displayed.entries.length" class="text-fg-muted p-2">
                    No folders here.
                </p>
                <ul v-else-if="displayed">
                    <li
                        v-for="{ entry, warning } in rows"
                        :key="entry.path"
                        class="flex items-center gap-1"
                    >
                        <ACheckbox
                            v-if="multiple"
                            :model-value="isSelected(entry.path)"
                            :label="`Select ${entry.name}`"
                            hide-label
                            class="shrink-0 pl-1"
                            @update:model-value="toggleFolder(entry.path)"
                        />
                        <button
                            data-row
                            :data-path="entry.path"
                            type="button"
                            :class="[rowClass, 'flex-1']"
                            @click="enter(entry.path)"
                            @keydown.space="onRowSpace($event, entry.path)"
                        >
                            <AIcon :icon="IconFolder" class="shrink-0 text-lg" />
                            <span class="truncate">{{ entry.name }}</span>
                            <AIcon
                                v-if="entry.symlink"
                                :icon="IconLink"
                                label="Symlink"
                                class="text-fg-muted shrink-0"
                            />
                            <span
                                v-if="warning"
                                :class="[warningClass, 'ml-auto max-w-1/2']"
                                :title="warning.long"
                            >
                                {{ warning.short }}
                            </span>
                        </button>
                    </li>
                </ul>
            </div>

            <ul
                v-if="multiple && basketOpen"
                ref="basketEl"
                class="flex flex-col"
                aria-label="Selected folders"
            >
                <li
                    v-for="{ path, warning } in basket"
                    :key="path"
                    class="flex items-center gap-2 pl-2"
                >
                    <div class="flex min-w-0 flex-1 flex-col">
                        <PathText :path="path" class="text-sm" />
                        <span v-if="warning" :class="warningClass" :title="warning.long">
                            {{ warning.short }}
                        </span>
                    </div>
                    <AIconButton
                        :icon="IconClose"
                        :label="`Remove ${path}`"
                        size="sm"
                        @click="toggleFolder(path)"
                    />
                </li>
            </ul>
        </div>

        <template #actions>
            <template v-if="multiple">
                <AButton
                    variant="text"
                    tone="neutral"
                    class="mr-auto"
                    :disabled="!selected.length"
                    :trailing-icon="basketOpen ? IconChevronUp : IconChevronDown"
                    :aria-expanded="basketOpen"
                    @click="basketOpen = !basketOpen"
                >
                    {{ selected.length }} selected
                </AButton>
                <AButton variant="text" tone="neutral" @click="close(null)">Cancel</AButton>
                <AButton
                    variant="tonal"
                    :disabled="!target || isSelected(target)"
                    @click="target && select(target)"
                >
                    Select this folder
                </AButton>
                <AButton :disabled="!basketPaths" @click="basketPaths && close(basketPaths)">
                    {{ addLabel }}
                </AButton>
            </template>
            <template v-else>
                <PathText
                    v-if="target"
                    :path="target"
                    class="text-fg-muted mr-auto min-w-0 flex-1 self-center text-sm"
                />
                <AButton variant="text" tone="neutral" @click="close(null)">Cancel</AButton>
                <AButton :disabled="!target" @click="target && close([target])">
                    Use this folder
                </AButton>
            </template>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, useTemplateRef, watch } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AAlert from '@/ui/AAlert.vue'
import AButton from '@/ui/AButton.vue'
import ACheckbox from '@/ui/ACheckbox.vue'
import ADialog from '@/ui/ADialog.vue'
import AIcon from '@/ui/AIcon.vue'
import AIconButton from '@/ui/AIconButton.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ASwitch from '@/ui/ASwitch.vue'
import ATextField from '@/ui/ATextField.vue'
import {
    IconArrowUp,
    IconChevronDown,
    IconChevronRight,
    IconChevronUp,
    IconClose,
    IconFolder,
    IconHardDrive,
    IconHome,
    IconLink,
    IconPencil,
    IconRefresh,
} from '@/ui/icons'
import { fsApi } from '@/utils/api/fs'
import type { LabeledPath } from '@/utils/sourceOverlap'
import PathText from './PathText.vue'
import { useFolderBrowser } from './useFolderBrowser'
import { useSourceOverlaps } from './useSourceOverlaps'

const props = defineProps<
    FolderPickerProps & { open: boolean; close: (paths: string[] | null) => void }
>()

const {
    requested,
    hidden,
    shown,
    displayed,
    loading,
    error,
    target,
    resolvedTo,
    parent,
    notice,
    draft,
    editing,
    errorUpdatedAt,
    refetch,
    navigate,
    goHome,
    open: openAt,
    startEditing,
    submitDraft,
} = useFolderBrowser()
const roots = fsApi.useRoots()
const overlaps = useSourceOverlaps(props.excludeLibraryId)
const rowClass = 'a-state a-focus flex min-w-0 items-center gap-2 rounded-lg px-2 py-1.5 text-left'
const warningClass = 'text-warning truncate text-xs'

const selected = ref<string[]>([])
const basketOpen = ref(false)
const listEl = useTemplateRef('listEl')
const pathInput = useTemplateRef('pathInput')
const crumbNav = useTemplateRef('crumbNav')
const basketEl = useTemplateRef('basketEl')

const crumbs = computed(() => {
    const path = displayed.value?.path ?? requested.value
    if (!path?.startsWith('/')) return []
    const names = path.split('/').filter(Boolean)
    return [
        { name: '/', path: '/' },
        ...names.map((name, i) => ({ name, path: '/' + names.slice(0, i + 1).join('/') })),
    ]
})

// Compares resolved paths: a folder may be selected through a symlink.
function sameFolder(a: string, b: string) {
    const real = (p: string) => overlaps.resolved(p)?.path ?? p
    return real(a) === real(b)
}

const isSelected = (path: string) => selected.value.some(p => sameFolder(p, path))

function select(path: string) {
    selected.value = [...selected.value, path]
}

function toggleFolder(path: string) {
    if (isSelected(path)) selected.value = selected.value.filter(p => !sameFolder(p, path))
    else select(path)
}

const formSources = computed(() =>
    props.otherSourcePaths.map(path => ({ path, label: 'this library' }))
)

// Selected folders aren't overlaps of themselves.
function othersFor(raw: string): LabeledPath[] {
    return [
        ...formSources.value,
        ...selected.value
            .filter(p => !sameFolder(p, raw))
            .map(path => ({ path, label: 'your selection' })),
    ]
}

watch(
    () => [
        ...props.otherSourcePaths,
        ...selected.value,
        ...(displayed.value?.entries.filter(e => e.symlink).map(e => e.path) ?? []),
    ],
    paths => overlaps.resolve(paths),
    { immediate: true }
)

// Not against the selection: each ticked child would add a sentence to a live region.
const currentWarning = computed(() =>
    displayed.value && !loading.value
        ? overlaps.warning(displayed.value.path, formSources.value)
        : undefined
)

const rows = computed(() =>
    (displayed.value?.entries ?? []).map(entry => ({
        entry,
        warning: entry.symlink
            ? overlaps.rawWarning(entry.path, othersFor(entry.path))
            : overlaps.warning(entry.path, othersFor(entry.path)),
    }))
)

const basket = computed(() =>
    selected.value.map(path => ({ path, warning: overlaps.rawWarning(path, othersFor(path)) }))
)

const addLabel = computed(() => {
    const n = selected.value.length
    return n === 0 ? 'Add' : n === 1 ? 'Add folder' : `Add ${n} folders`
})

/** The selection as resolved paths, once all of them are. */
const basketPaths = computed(() => {
    const paths = selected.value.map(p => overlaps.resolved(p)?.path)
    if (!paths.length || !paths.every(p => p != null)) return null
    return [...new Set(paths)]
})

// Focus follows navigation into the list: onto the row for `path` if there is one (the folder just
// left, or the one that failed to open), else the first row. `fromPathBar` sends a failed typed
// path back to the path bar.
let focusList: { path?: string; fromPathBar?: boolean } | null = null

function enter(path: string, resolve = false) {
    focusList = { path }
    if (resolve) openAt(path)
    else navigate(path)
}

function goUp() {
    if (!parent.value) return
    focusList = { path: displayed.value?.path ?? requested.value ?? undefined }
    navigate(parent.value)
}

function focusRow(path?: string) {
    const el = listEl.value
    if (!el) return
    const row = path && el.querySelector<HTMLElement>(`[data-path="${CSS.escape(path)}"]`)
    ;(row || el.querySelector<HTMLElement>('[data-row]') || el).focus()
}

// `shown` is replaced on every load, even when returning to the same cached listing.
watch(
    shown,
    () => {
        if (listEl.value) listEl.value.scrollTop = 0
        if (!focusList) return
        focusRow(focusList.path)
        focusList = null
    },
    { flush: 'post' }
)

// Keyed on the error's time, so failing again with the same message still moves focus. Post-flush:
// the rows replace the spinner first.
watch(
    errorUpdatedAt,
    () => {
        if (!error.value || !focusList) return
        if (focusList.fromPathBar) edit()
        else focusRow(focusList.path)
        focusList = null
    },
    { flush: 'post' }
)

// Long paths keep the current folder in view.
watch(
    [crumbs, editing],
    () => {
        if (crumbNav.value) crumbNav.value.scrollLeft = crumbNav.value.scrollWidth
    },
    { flush: 'post' }
)

watch(
    basketOpen,
    open => {
        if (open) basketEl.value?.scrollIntoView({ block: 'nearest' })
    },
    { flush: 'post' }
)

function refresh() {
    overlaps.retry()
    if (requested.value === null) roots.refetch()
    else refetch()
}

async function edit() {
    startEditing()
    await nextTick()
    pathInput.value?.focus()
}

function submitPath() {
    if (submitDraft()) focusList = { fromPathBar: true }
    else stopEditing()
}

// Esc only leaves the path input; preventing it keeps the dialog open.
async function stopEditing(e?: KeyboardEvent) {
    e?.preventDefault()
    editing.value = false
    await nextTick()
    listEl.value?.focus()
}

// Space toggles a row's checkbox; the name button still opens the folder on Enter.
function onRowSpace(e: KeyboardEvent, path: string) {
    if (!props.multiple) return
    e.preventDefault()
    toggleFolder(path)
}

function onListKeydown(e: KeyboardEvent) {
    const rows = [...(listEl.value?.querySelectorAll<HTMLElement>('[data-row]') ?? [])]
    const current = rows.findIndex(r => (r.closest('li') ?? r).contains(document.activeElement))
    let next: number
    switch (e.key) {
        case 'ArrowDown':
            next = Math.min(current + 1, rows.length - 1)
            break
        case 'ArrowUp':
            next = Math.max(current - 1, 0)
            break
        case 'Home':
            next = 0
            break
        case 'End':
            next = rows.length - 1
            break
        case 'Backspace':
            e.preventDefault()
            goUp()
            return
        default:
            return
    }
    e.preventDefault()
    rows[next]?.focus()
}

onMounted(() => {
    if (props.initialPath) {
        focusList = {}
        openAt(props.initialPath)
    }
})
</script>

<script lang="ts">
import { Modals, type ShowOptions } from '@/utils/modals'
import Self from './FolderPickerModal.vue'

export type FolderPickerProps = {
    initialPath?: string
    multiple?: boolean
    excludeLibraryId?: string
    /** The form's other rows (minus the one being replaced), for overlap warnings. */
    otherSourcePaths: string[]
}

/** Resolves to absolute, symlink-free folder paths, or `null` when dismissed. */
export function showFolderPicker(
    props: FolderPickerProps,
    options?: ShowOptions
): Promise<string[] | null> {
    return Modals.show<string[] | null>(Self, props, options)
}
</script>
