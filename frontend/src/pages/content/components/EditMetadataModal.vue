<template>
    <ADialog :open="open" title="Edit metadata" size="lg" @update:open="v => !v && close()">
        <div class="flex flex-col gap-5">
            <QueryError :query="qLayers" />

            <div v-if="qLayers.isLoading.value" class="flex justify-center py-10">
                <ASpinner />
            </div>

            <template v-else-if="qLayers.isSuccess.value && localLayers">
                <div class="flex flex-wrap items-center gap-2">
                    <ASelect
                        ref="viewSelect"
                        v-model="selectedView"
                        :options="viewOptions"
                        label="Source"
                        class="w-48"
                    />
                    <AButton
                        v-if="selectedViewLayer && selectedViewLayer.source !== 'overrides'"
                        variant="tonal"
                        :leading-icon="IconCodeJson"
                        @click="showRawDialog = true"
                    >
                        Raw data
                    </AButton>
                    <AButton
                        v-if="selectedView === 'mangabaka'"
                        variant="tonal"
                        tone="danger"
                        :leading-icon="IconLinkOff"
                        :loading="mUnlink.isPending.value"
                        @click="mUnlink.mutate()"
                    >
                        Unlink
                    </AButton>
                </div>

                <dl v-if="fieldsWithValues.length" class="flex flex-col">
                    <div
                        v-for="field in fieldsWithValues"
                        :key="field.key"
                        class="border-outline-variant flex items-start gap-x-4 border-t py-1.5 pl-1 max-sm:flex-wrap"
                    >
                        <dt class="flex min-h-8 w-40 shrink-0 flex-wrap items-center gap-1.5">
                            {{ field.label }}
                            <AChip
                                v-if="
                                    selectedView === 'merged' &&
                                    currentView.sources[field.key] !== undefined
                                "
                                size="sm"
                                :tone="
                                    currentView.sources[field.key] === 'overrides'
                                        ? 'primary'
                                        : 'neutral'
                                "
                            >
                                {{ sourceLabel(currentView.sources[field.key]!) }}
                            </AChip>
                        </dt>
                        <dd
                            class="text-fg-muted min-w-0 flex-1 py-1.5 [overflow-wrap:anywhere] whitespace-pre-wrap max-sm:order-last max-sm:basis-full max-sm:pt-0"
                        >
                            {{ formatValue(currentView.values[field.key]) }}
                        </dd>
                        <AIconButton
                            v-if="isEditable"
                            :icon="IconPencil"
                            :id="`${editIdPrefix}-${field.key}`"
                            :label="`Edit ${field.label}`"
                            size="sm"
                            class="max-sm:ml-auto"
                            @click="openFieldEditor(field.key)"
                        />
                    </div>
                </dl>
                <p v-else class="text-fg-muted">No metadata from this source.</p>

                <section
                    v-if="isEditable && fieldsWithoutValues.length"
                    class="flex flex-col gap-2"
                >
                    <h3 class="text-fg-muted text-xs font-semibold tracking-wide">Add a field</h3>
                    <div class="flex flex-wrap gap-1.5">
                        <AChip
                            v-for="field in fieldsWithoutValues"
                            :key="field.key"
                            variant="outlined"
                            size="sm"
                            :leading-icon="IconPlus"
                            @click="openFieldEditor(field.key)"
                        >
                            {{ field.label }}
                        </AChip>
                    </div>
                </section>

                <section v-if="isEditable && isSeriesType" class="flex flex-col gap-2">
                    <h3 class="text-fg-muted text-xs font-semibold tracking-wide">Link a source</h3>
                    <div class="flex flex-wrap gap-1.5">
                        <AChip
                            :variant="hasMangabakaLayer ? 'tonal' : 'outlined'"
                            :tone="hasMangabakaLayer ? 'primary' : 'neutral'"
                            size="sm"
                            :leading-icon="hasMangabakaLayer ? IconCheck : IconLink"
                            @click="onMangaBakaChipClick()"
                        >
                            MangaBaka
                            <span class="sr-only">
                                {{ hasMangabakaLayer ? '(linked, show its data)' : '(search)' }}
                            </span>
                        </AChip>
                    </div>
                </section>
            </template>

            <QueryError :mutation="mSave" />
            <QueryError :mutation="mUnlink" />
        </div>

        <template #actions>
            <AButton variant="text" tone="neutral" @click="close()">Close</AButton>
            <AButton
                v-if="isEditable"
                :disabled="!isDirty"
                focusable-when-disabled
                :loading="mSave.isPending.value"
                @click="mSave.mutate()"
            >
                Save
            </AButton>
        </template>

        <!-- Nested here so they stack on this dialog and return focus to it. -->
        <ADialog
            v-model:open="fieldDialogOpen"
            :title="`Edit ${editingFieldDef?.label ?? ''}`"
            size="md"
        >
            <template v-if="editingFieldDef">
                <ATextField
                    v-if="editingFieldDef.type === 'text'"
                    v-model="editValue"
                    :label="editingFieldDef.label"
                    :hint="
                        editingFieldDef.key === 'staff' ? 'One per line: Name (role)' : undefined
                    "
                    multiline
                    auto-grow
                    autofocus
                />
                <ATextField
                    v-else
                    v-model="editValue"
                    :label="editingFieldDef.label"
                    :inputmode="editingFieldDef.type === 'number' ? 'decimal' : undefined"
                    :error="editError"
                    autofocus
                    @keydown.enter="onEditorEnter"
                />
            </template>
            <template #actions>
                <AButton
                    v-if="editingFieldDef && hasOverride(editingFieldDef.key)"
                    variant="text"
                    tone="danger"
                    class="mr-auto"
                    @click="removeOverride()"
                >
                    Remove override
                </AButton>
                <AButton variant="text" tone="neutral" @click="fieldDialogOpen = false">
                    Cancel
                </AButton>
                <AButton @click="confirmFieldEdit()">OK</AButton>
            </template>
        </ADialog>

        <ADialog
            v-model:open="showRawDialog"
            :title="`Raw data: ${selectedViewLayer ? sourceLabel(selectedViewLayer.source) : ''}`"
            size="xl"
        >
            <div v-if="selectedViewLayer" class="flex flex-col gap-4 lg:flex-row">
                <section class="flex min-w-0 flex-1 flex-col gap-1">
                    <h3 class="text-fg-muted text-xs font-semibold tracking-wide">
                        Normalized data
                    </h3>
                    <pre class="raw-json">{{
                        JSON.stringify(selectedViewLayer.data, null, 2)
                    }}</pre>
                </section>
                <section
                    v-if="Object.keys(selectedViewLayer.raw).length"
                    class="flex min-w-0 flex-1 flex-col gap-1"
                >
                    <h3 class="text-fg-muted text-xs font-semibold tracking-wide">Raw response</h3>
                    <pre class="raw-json">{{ JSON.stringify(selectedViewLayer.raw, null, 2) }}</pre>
                </section>
            </div>
            <template #actions>
                <AButton variant="text" tone="neutral" @click="showRawDialog = false">
                    Close
                </AButton>
            </template>
        </ADialog>
    </ADialog>
</template>

<script setup lang="ts">
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { computed, nextTick, ref, useId, useTemplateRef, watch } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import AChip from '@/ui/AChip.vue'
import ADialog from '@/ui/ADialog.vue'
import AIconButton from '@/ui/AIconButton.vue'
import ASelect from '@/ui/ASelect.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconCheck, IconCodeJson, IconLink, IconLinkOff, IconPencil, IconPlus } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { contentApi } from '@/utils/api/content'
import { metadataSourcesApi } from '@/utils/api/metadata-sources'
import type { ContentMetadata, MetadataLayersResponse } from '@/utils/api/types'

const props = defineProps<{
    open: boolean
    close: () => void
    contentId: string
}>()

const queryClient = useQueryClient()
const toast = useToast()
const viewSelect = useTemplateRef('viewSelect')
const qContent = contentApi.useGet(() => props.contentId)
const qLayers = contentApi.useMetadataLayers(() => props.contentId)

const SOURCE_LABELS: Record<string, string> = {
    file: 'File',
    mangabaka: 'MangaBaka',
    overrides: 'Overrides',
}

const SOURCE_ORDER = ['file', 'mangabaka', 'overrides']

function sourceLabel(source: string): string {
    return SOURCE_LABELS[source] ?? source
}

interface FieldDef {
    key: keyof ContentMetadata
    label: string
    type: 'string' | 'number' | 'text'
}

const metadataFields: FieldDef[] = [
    { key: 'title', label: 'Title', type: 'string' },
    { key: 'series', label: 'Series', type: 'string' },
    { key: 'number', label: 'Number', type: 'string' },
    { key: 'volume', label: 'Volume', type: 'number' },
    { key: 'count', label: 'Count', type: 'number' },
    { key: 'staff', label: 'Staff', type: 'text' },
    { key: 'publisher', label: 'Publisher', type: 'string' },
    { key: 'imprint', label: 'Imprint', type: 'string' },
    { key: 'description', label: 'Description', type: 'text' },
    { key: 'genre', label: 'Genre', type: 'string' },
    { key: 'age_rating', label: 'Age Rating', type: 'string' },
    { key: 'language', label: 'Language', type: 'string' },
    { key: 'publication_date', label: 'Publication Date', type: 'string' },
    { key: 'manga', label: 'Manga', type: 'string' },
    { key: 'series_group', label: 'Series Group', type: 'string' },
    { key: 'format', label: 'Format', type: 'string' },
    { key: 'web', label: 'Web', type: 'string' },
    { key: 'notes', label: 'Notes', type: 'text' },
    { key: 'scan_information', label: 'Scan Information', type: 'string' },
    { key: 'black_and_white', label: 'Black & White', type: 'string' },
    { key: 'alternate_series', label: 'Alternate Series', type: 'string' },
    { key: 'alternate_number', label: 'Alternate Number', type: 'string' },
    { key: 'alternate_count', label: 'Alternate Count', type: 'number' },
]

const localLayers = ref<MetadataLayersResponse | null>(null)
const serverSnapshot = ref<string>('')
const selectedView = ref<string>('merged')
/** Kept after the field dialog closes, so its content stays put while it animates out. */
const editingField = ref<keyof ContentMetadata | null>(null)
const fieldDialogOpen = ref(false)
const editValue = ref<string>('')
const showRawDialog = ref(false)

/** Takes the server's layers. `keepEdits` keeps unsaved override edits (a MangaBaka link refresh). */
function applyLayers(response: MetadataLayersResponse, keepEdits: boolean) {
    const data = jsonClone(response)
    if (!data.layers.some(l => l.source === 'overrides')) {
        data.layers.push({ source: 'overrides', data: {}, raw: {} })
    }
    const serverOverrides = data.layers.find(l => l.source === 'overrides')!
    const localOverrides = localLayers.value?.layers.find(l => l.source === 'overrides')
    if (keepEdits && localOverrides && overridesKey(localOverrides.data) !== serverSnapshot.value) {
        data.layers[data.layers.indexOf(serverOverrides)] = localOverrides
    }
    serverSnapshot.value = overridesKey(serverOverrides.data)
    localLayers.value = data
}

watch(
    () => qLayers.data?.value,
    data => data && applyLayers(data, true),
    { immediate: true }
)

const overridesLayer = computed(() => localLayers.value?.layers.find(l => l.source === 'overrides'))

const currentView = computed(() => {
    if (!localLayers.value)
        return { values: {} as Record<string, any>, sources: {} as Record<string, string> }
    const layers =
        selectedView.value === 'merged'
            ? localLayers.value.layers
            : localLayers.value.layers.filter(l => l.source === selectedView.value)
    const values: Record<string, any> = {}
    const sources: Record<string, string> = {}
    const sorted = [...layers].sort(
        (a, b) => SOURCE_ORDER.indexOf(a.source) - SOURCE_ORDER.indexOf(b.source)
    )
    for (const layer of sorted) {
        for (const [key, val] of Object.entries(layer.data)) {
            if (val != null) {
                values[key] = val
                sources[key] = layer.source
            }
        }
    }
    return { values, sources }
})

const viewOptions = computed(() => {
    const layers = localLayers.value?.layers ?? []
    const options = [{ label: 'Merged', value: 'merged' }]
    for (const layer of layers) {
        if (layer.source !== 'overrides' && !Object.keys(layer.data).length) continue
        options.push({ label: sourceLabel(layer.source), value: layer.source })
    }
    if (!options.some(o => o.value === 'overrides')) {
        options.push({ label: sourceLabel('overrides'), value: 'overrides' })
    }
    return options
})

const selectedViewLayer = computed(() => {
    if (selectedView.value === 'merged') return null
    return localLayers.value?.layers.find(l => l.source === selectedView.value) ?? null
})

const isEditable = computed(
    () => selectedView.value === 'merged' || selectedView.value === 'overrides'
)

const isDirty = computed(() => overridesKey(overridesLayer.value?.data) !== serverSnapshot.value)

const isSeriesType = computed(() => {
    const t = qContent.data?.value?.type
    return t === 'comic_series' || t === 'book_series'
})

const mangabakaLayer = computed(() => localLayers.value?.layers.find(l => l.source === 'mangabaka'))

const hasMangabakaLayer = computed(() => {
    const data = mangabakaLayer.value?.data
    return data != null && typeof data === 'object' && Object.keys(data).length > 0
})

async function onMangaBakaChipClick() {
    if (hasMangabakaLayer.value) {
        selectedView.value = 'mangabaka'
        // The chip is gone in this view: keep focus in the dialog.
        await nextTick()
        viewSelect.value?.focus()
    } else {
        showSearchMangaBakaModal(props.contentId)
    }
}

const mUnlink = useMutation({
    mutationFn: () => metadataSourcesApi.unlink(props.contentId, 'mangabaka'),
    async onSuccess() {
        selectedView.value = 'merged'
        toast.show({ message: 'Unlinked MangaBaka' })
        // The Unlink button is gone: keep focus in the dialog.
        await nextTick()
        viewSelect.value?.focus()
        queryClient.invalidateQueries({ queryKey: ['content', 'metadata-layers', props.contentId] })
        queryClient.invalidateQueries({ queryKey: ['content', props.contentId] })
    },
})

const editingFieldDef = computed(() =>
    editingField.value ? (metadataFields.find(f => f.key === editingField.value) ?? null) : null
)

const fieldsWithValues = computed(() =>
    metadataFields.filter(f => formatValue(currentView.value.values[f.key]) !== '')
)

const fieldsWithoutValues = computed(() =>
    metadataFields
        .filter(f => formatValue(currentView.value.values[f.key]) === '')
        .sort((a, b) => a.label.localeCompare(b.label))
)

function formatValue(val: any): string {
    if (val == null) return ''
    if (Array.isArray(val)) {
        // Staff entries: [{name, role}, ...]
        if (val.length > 0 && typeof val[0] === 'object' && 'name' in val[0]) {
            return val.map((e: any) => `${e.name} (${e.role})`).join(', ')
        }
        return val.join(', ')
    }
    return String(val)
}

function hasOverride(key: keyof ContentMetadata): boolean {
    const val = (overridesLayer.value?.data as any)?.[key]
    return val != null
}

const editIdPrefix = useId()
/** The pre-filled value, so confirming it unchanged doesn't create an override. */
let initialEditValue = ''

const editError = computed(() =>
    editingFieldDef.value?.type === 'number' &&
    editValue.value.trim() !== '' &&
    !Number.isInteger(Number(editValue.value))
        ? 'Enter a whole number'
        : undefined
)

function openFieldEditor(key: keyof ContentMetadata) {
    editingField.value = key
    fieldDialogOpen.value = true
    // The override if there is one, else the value shown in this view.
    const val = (overridesLayer.value?.data as any)?.[key] ?? currentView.value.values[key]
    if (key === 'staff' && Array.isArray(val)) {
        // One entry per line: "name (role)"
        editValue.value = val.map((e: any) => `${e.name} (${e.role})`).join('\n')
    } else {
        editValue.value = val != null ? formatValue(val) : ''
    }
    initialEditValue = editValue.value
}

// The editor's opener (a chip, or a row that loses its value) can be gone once it closes.
async function closeFieldEditor(key: keyof ContentMetadata) {
    fieldDialogOpen.value = false
    await nextTick()
    const button = document.getElementById(`${editIdPrefix}-${key}`)
    if (button) button.focus()
    else viewSelect.value?.focus()
}

const staffLineRe = /^(.+?)\s*\(([^)]+)\)\s*$/

// Not while an IME composition is open. `preventDefault`: otherwise the Enter's keypress activates
// the Edit button focus returns to.
function onEditorEnter(e: KeyboardEvent) {
    if (e.isComposing || e.keyCode === 229) return
    e.preventDefault()
    confirmFieldEdit()
}

function confirmFieldEdit() {
    if (!fieldDialogOpen.value || !editingField.value || !overridesLayer.value) return
    if (editError.value) return
    const key = editingField.value
    const data = overridesLayer.value.data as Record<string, any>
    if (editValue.value === initialEditValue && !hasOverride(key)) {
        // Unchanged: nothing to override.
    } else if (editValue.value === '') {
        delete data[editingField.value]
    } else if (editingField.value === 'staff') {
        // Parse "name (role)" lines
        const entries = editValue.value
            .split('\n')
            .map(line => line.trim())
            .filter(Boolean)
            .map(line => {
                const m = staffLineRe.exec(line)
                return m ? { name: m[1], role: m[2] } : { name: line, role: 'author' }
            })
        if (entries.length > 0) {
            data[editingField.value] = entries
        } else {
            delete data[editingField.value]
        }
    } else {
        data[editingField.value] = editValue.value
    }
    closeFieldEditor(key)
}

function removeOverride() {
    if (!editingField.value || !overridesLayer.value) return
    delete (overridesLayer.value.data as Record<string, any>)[editingField.value]
    closeFieldEditor(editingField.value)
}

/** The overrides as the server stores them (numbers parsed, empty values dropped). */
function overridesPayload(raw: ContentMetadata | undefined): Record<string, any> {
    const payload: Record<string, any> = {}
    for (const field of metadataFields) {
        const val = (raw as any)?.[field.key]
        if (val == null || val === '') continue
        if (field.key === 'staff') {
            // Already stored as parsed array in the override layer
            if (Array.isArray(val) && val.length > 0) payload[field.key] = val
        } else if (field.type === 'number') {
            const num = Number(val)
            if (!isNaN(num)) payload[field.key] = num
        } else {
            payload[field.key] = val
        }
    }
    return payload
}

/** Compares overrides by what would be saved, ignoring key order (jsonb reorders keys). */
function overridesKey(raw: ContentMetadata | undefined): string {
    const sortKeys = (v: unknown): unknown =>
        Array.isArray(v)
            ? v.map(sortKeys)
            : v && typeof v === 'object'
              ? Object.fromEntries(
                    Object.keys(v)
                        .sort()
                        .map(k => [k, sortKeys((v as any)[k])])
                )
              : v
    return JSON.stringify(sortKeys(overridesPayload(raw)))
}

const mSave = useMutation({
    mutationFn: () =>
        contentApi.updateMetadataOverride(
            props.contentId,
            overridesPayload(overridesLayer.value?.data) as ContentMetadata
        ),
    onSuccess(response) {
        applyLayers(response, false)
        toast.show({ message: 'Saved the metadata' })
        queryClient.invalidateQueries({ queryKey: ['content', 'metadata-layers', props.contentId] })
        queryClient.invalidateQueries({ queryKey: ['content', props.contentId] })
    },
})
</script>

<script lang="ts">
import { jsonClone } from '@/utils/misc'
import { Modals } from '@/utils/modals'
import Self from './EditMetadataModal.vue'
import { showSearchMangaBakaModal } from './SearchMangaBakaModal.vue'

export function showEditMetadataModal(contentId: string): Promise<void> {
    return Modals.show(Self, { contentId })
}
</script>

<style scoped>
@layer ui {
    .raw-json {
        max-height: 60dvh;
        overflow: auto;
        padding: 12px;
        border-radius: var(--radius-field);
        background: var(--color-surface-2);
        font-size: 12px;
        line-height: 1.4;
        white-space: pre-wrap;
        word-break: break-word;
    }
}
</style>
