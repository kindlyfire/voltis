<template>
    <VDialog :model-value="open" @update:model-value="v => !v && close()" max-width="700">
        <VCard>
            <VCardTitle>Edit metadata</VCardTitle>
            <VCardText class="space-y-4!">
                <AQueryError :query="qLayers" />

                <div v-if="qLayers.isLoading.value" class="py-10 text-center">
                    <VProgressCircular indeterminate />
                </div>

                <template v-else-if="qLayers.isSuccess.value && localLayers">
                    <div class="d-flex align-center ga-2">
                        <VSelect
                            v-model="selectedView"
                            :items="viewOptions"
                            item-title="title"
                            item-value="value"
                            density="compact"
                            variant="outlined"
                            hide-details
                        />
                        <VBtn
                            v-if="selectedViewLayer && selectedViewLayer.source !== 'overrides'"
                            variant="tonal"
                            @click="showRawDialog = true"
                            class="h-10!"
                        >
                            Raw
                        </VBtn>
                        <VBtn
                            v-if="selectedView === 'mangabaka'"
                            variant="tonal"
                            color="error"
                            :loading="mUnlink.isPending.value"
                            @click="mUnlink.mutate()"
                            class="h-10!"
                        >
                            Unlink
                        </VBtn>
                    </div>

                    <div class="metadata-fields">
                        <template v-for="field in fieldsWithValues" :key="field.key">
                            <div class="metadata-field d-flex align-center ga-2 py-1">
                                <div
                                    class="text-caption text-medium-emphasis field-label d-flex align-center"
                                    :class="{ 'field-label--editable': isEditable }"
                                    @click="isEditable && openFieldEditor(field.key)"
                                >
                                    {{ field.label }}
                                    <VChip
                                        v-if="
                                            selectedView === 'merged' &&
                                            currentView.sources[field.key] !== undefined
                                        "
                                        size="x-small"
                                        variant="tonal"
                                        :color="
                                            currentView.sources[field.key] === 'overrides'
                                                ? 'primary'
                                                : undefined
                                        "
                                        class="ml-1"
                                    >
                                        {{ sourceLabel(currentView.sources[field.key]!) }}
                                    </VChip>
                                </div>
                                <div class="field-value grow">
                                    <span class="text-body-2">
                                        {{ formatValue(currentView.values[field.key]) }}
                                    </span>
                                </div>
                            </div>
                        </template>
                    </div>

                    <div v-if="isEditable && fieldsWithoutValues.length" class="mt-2">
                        <span class="text-caption text-medium-emphasis">Add:</span>
                        <div class="d-flex ga-1 mt-1 flex-wrap">
                            <VChip
                                v-for="field in fieldsWithoutValues"
                                :key="field.key"
                                size="small"
                                class="cursor-pointer"
                                @click="openFieldEditor(field.key)"
                            >
                                {{ field.label }}
                            </VChip>
                        </div>
                    </div>

                    <div v-if="isEditable && isSeriesType" class="mt-2">
                        <span class="text-caption text-medium-emphasis">Link Source:</span>
                        <div class="d-flex ga-1 mt-1 flex-wrap">
                            <VChip
                                size="small"
                                class="cursor-pointer"
                                :color="hasMangabakaLayer ? 'primary' : undefined"
                                @click="onMangaBakaChipClick()"
                            >
                                MangaBaka
                            </VChip>
                        </div>
                    </div>
                </template>

                <AQueryError :mutation="mSave" />
            </VCardText>
            <VCardActions>
                <VSpacer />
                <VBtn variant="text" @click="close()">Close</VBtn>
                <VBtn
                    v-if="isEditable && isDirty"
                    color="primary"
                    :loading="mSave.isPending.value"
                    @click="mSave.mutate()"
                >
                    Save
                </VBtn>
            </VCardActions>
        </VCard>
    </VDialog>

    <!-- Field edit sub-dialog -->
    <VDialog
        :model-value="editingField != null"
        @update:model-value="v => !v && (editingField = null)"
        max-width="500"
    >
        <VCard v-if="editingFieldDef">
            <VCardTitle>Edit {{ editingFieldDef.label }}</VCardTitle>
            <VCardText>
                <VTextarea
                    v-if="editingFieldDef.type === 'text'"
                    v-model="editValue"
                    variant="outlined"
                    hide-details
                    rows="3"
                    auto-grow
                    autofocus
                />
                <VTextField
                    v-else
                    v-model="editValue"
                    :type="editingFieldDef.type === 'number' ? 'number' : 'text'"
                    variant="outlined"
                    hide-details
                    autofocus
                    @keydown.enter="confirmFieldEdit()"
                />
            </VCardText>
            <VCardActions>
                <VBtn
                    v-if="hasOverride(editingFieldDef.key)"
                    variant="text"
                    color="error"
                    @click="removeOverride()"
                >
                    Remove override
                </VBtn>
                <VSpacer />
                <VBtn variant="text" @click="editingField = null">Cancel</VBtn>
                <VBtn color="primary" variant="flat" @click="confirmFieldEdit()">OK</VBtn>
            </VCardActions>
        </VCard>
    </VDialog>

    <!-- Raw data sub-dialog -->
    <VDialog v-model="showRawDialog" max-width="1000">
        <VCard v-if="selectedViewLayer">
            <VCardTitle>Raw data — {{ sourceLabel(selectedViewLayer.source) }}</VCardTitle>
            <VCardText>
                <div class="flex flex-col gap-4 lg:flex-row">
                    <div class="flex-1">
                        <div class="text-caption text-medium-emphasis mb-1">Normalized Data</div>
                        <pre class="raw-json">{{
                            JSON.stringify(selectedViewLayer.data, null, 2)
                        }}</pre>
                    </div>
                    <div v-if="Object.keys(selectedViewLayer.raw).length" class="flex-1">
                        <div class="text-caption text-medium-emphasis mb-1">Raw Response</div>
                        <pre class="raw-json">{{
                            JSON.stringify(selectedViewLayer.raw, null, 2)
                        }}</pre>
                    </div>
                </div>
            </VCardText>
            <VCardActions>
                <VSpacer />
                <VBtn variant="text" @click="showRawDialog = false">Close</VBtn>
            </VCardActions>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'
import AQueryError from '@/components/AQueryError.vue'
import { contentApi } from '@/utils/api/content'
import { metadataSourcesApi } from '@/utils/api/metadata-sources'
import type { ContentMetadata, MetadataLayersResponse } from '@/utils/api/types'

const props = defineProps<{
    open: boolean
    close: () => void
    contentId: string
}>()

const queryClient = useQueryClient()
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
const editingField = ref<keyof ContentMetadata | null>(null)
const editValue = ref<string>('')
const showRawDialog = ref(false)

watch(
    () => qLayers.data?.value,
    data => {
        if (!data) return
        localLayers.value = jsonClone(data)
        const serverOverrides = data.layers.find(l => l.source === 'overrides')
        serverSnapshot.value = JSON.stringify(serverOverrides?.data ?? {})
    },
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
    const options = [{ title: 'Merged', value: 'merged' }]
    for (const layer of layers) {
        if (layer.source !== 'overrides' && !Object.keys(layer.data).length) continue
        options.push({ title: sourceLabel(layer.source), value: layer.source })
    }
    if (!options.some(o => o.value === 'overrides')) {
        options.push({ title: sourceLabel('overrides'), value: 'overrides' })
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

const isDirty = computed(
    () => JSON.stringify(overridesLayer.value?.data ?? {}) !== serverSnapshot.value
)

const isSeriesType = computed(() => {
    const t = qContent.data?.value?.type
    return t === 'comic_series' || t === 'book_series'
})

const mangabakaLayer = computed(() => localLayers.value?.layers.find(l => l.source === 'mangabaka'))

const hasMangabakaLayer = computed(() => {
    const data = mangabakaLayer.value?.data
    return data != null && typeof data === 'object' && Object.keys(data).length > 0
})

function onMangaBakaChipClick() {
    if (hasMangabakaLayer.value) {
        selectedView.value = 'mangabaka'
    } else {
        showSearchMangaBakaModal(props.contentId)
    }
}

const mUnlink = useMutation({
    mutationFn: () => metadataSourcesApi.unlink(props.contentId, 'mangabaka'),
    onSuccess() {
        selectedView.value = 'merged'
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

function openFieldEditor(key: keyof ContentMetadata) {
    editingField.value = key
    const val = (overridesLayer.value?.data as any)?.[key]
    if (key === 'staff' && Array.isArray(val)) {
        // One entry per line: "name (role)"
        editValue.value = val.map((e: any) => `${e.name} (${e.role})`).join('\n')
    } else {
        editValue.value = val != null ? formatValue(val) : ''
    }
}

const staffLineRe = /^(.+?)\s*\(([^)]+)\)\s*$/

function confirmFieldEdit() {
    if (!editingField.value || !overridesLayer.value) return
    const data = overridesLayer.value.data as Record<string, any>
    if (editValue.value === '') {
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
    editingField.value = null
}

function removeOverride() {
    if (!editingField.value || !overridesLayer.value) return
    delete (overridesLayer.value.data as Record<string, any>)[editingField.value]
    editingField.value = null
}

const mSave = useMutation({
    mutationFn: async () => {
        const raw = overridesLayer.value?.data ?? {}
        const payload: Record<string, any> = {}
        for (const field of metadataFields) {
            const val = (raw as any)[field.key]
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
        return contentApi.updateMetadataOverride(props.contentId, payload as ContentMetadata)
    },
    onSuccess() {
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
.field-label {
    min-width: 140px;
    flex-shrink: 0;
}

.field-label--editable {
    cursor: pointer;
    border-radius: 4px;
}

.field-label--editable:hover {
    text-decoration: underline;
}

.raw-json {
    font-size: 0.75rem;
    line-height: 1.4;
    background: rgba(var(--v-theme-on-surface), 0.05);
    border-radius: 4px;
    padding: 12px;
    overflow: auto;
    max-height: 500px;
    white-space: pre-wrap;
    word-break: break-word;
}
</style>
