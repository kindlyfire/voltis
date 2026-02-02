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
                            v-if="selectedViewLayer && selectedViewLayer.provider !== 99"
                            variant="tonal"
                            @click="showRawDialog = true"
                            class="h-10!"
                        >
                            Raw
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
                                            currentView.sources[field.key] === 99
                                                ? 'primary'
                                                : undefined
                                        "
                                        class="ml-1"
                                    >
                                        {{ providerLabel(currentView.sources[field.key]!) }}
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
                        <div class="d-flex flex-wrap ga-1 mt-1">
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
            <VCardTitle>Raw data — {{ providerLabel(selectedViewLayer.provider) }}</VCardTitle>
            <VCardText>
                <div class="flex flex-col lg:flex-row gap-4">
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
import { computed, ref, watch } from 'vue'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { contentApi } from '@/utils/api/content'
import type { ContentMetadata, MetadataLayersResponse } from '@/utils/api/types'
import AQueryError from '@/components/AQueryError.vue'

const props = defineProps<{
    open: boolean
    close: () => void
    contentId: string
}>()

const queryClient = useQueryClient()
const qLayers = contentApi.useMetadataLayers(() => props.contentId)

const PROVIDER_LABELS: Record<number, string> = {
    0: 'File',
    1: 'MangaBaka',
    99: 'Overrides',
}

function providerLabel(provider: number): string {
    return PROVIDER_LABELS[provider] ?? `Provider ${provider}`
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
    { key: 'authors', label: 'Authors', type: 'string' },
    { key: 'writer', label: 'Writer', type: 'string' },
    { key: 'penciller', label: 'Penciller', type: 'string' },
    { key: 'inker', label: 'Inker', type: 'string' },
    { key: 'colorist', label: 'Colorist', type: 'string' },
    { key: 'letterer', label: 'Letterer', type: 'string' },
    { key: 'cover_artist', label: 'Cover Artist', type: 'string' },
    { key: 'editor', label: 'Editor', type: 'string' },
    { key: 'publisher', label: 'Publisher', type: 'string' },
    { key: 'imprint', label: 'Imprint', type: 'string' },
    { key: 'description', label: 'Description', type: 'text' },
    { key: 'genre', label: 'Genre', type: 'string' },
    { key: 'age_rating', label: 'Age Rating', type: 'string' },
    { key: 'language', label: 'Language', type: 'string' },
    { key: 'publication_date', label: 'Publication Date', type: 'string' },
    { key: 'manga', label: 'Manga', type: 'string' },
    { key: 'characters', label: 'Characters', type: 'string' },
    { key: 'teams', label: 'Teams', type: 'string' },
    { key: 'locations', label: 'Locations', type: 'string' },
    { key: 'story_arc', label: 'Story Arc', type: 'string' },
    { key: 'series_group', label: 'Series Group', type: 'string' },
    { key: 'format', label: 'Format', type: 'string' },
    { key: 'web', label: 'Web', type: 'string' },
    { key: 'notes', label: 'Notes', type: 'text' },
    { key: 'scan_information', label: 'Scan Information', type: 'string' },
    { key: 'black_and_white', label: 'Black & White', type: 'string' },
    { key: 'community_rating', label: 'Community Rating', type: 'number' },
    { key: 'review', label: 'Review', type: 'text' },
    { key: 'main_character_or_team', label: 'Main Character/Team', type: 'string' },
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
        localLayers.value = JSON.parse(JSON.stringify(data))
        const server99 = data.layers.find(l => l.provider === 99)
        serverSnapshot.value = JSON.stringify(server99?.data ?? {})
    },
    { immediate: true }
)

const overridesLayer = computed(() => localLayers.value?.layers.find(l => l.provider === 99))

const currentView = computed(() => {
    if (!localLayers.value)
        return { values: {} as Record<string, any>, sources: {} as Record<string, number> }
    const layers =
        selectedView.value === 'merged'
            ? localLayers.value.layers
            : localLayers.value.layers.filter(l => l.provider === Number(selectedView.value))
    const values: Record<string, any> = {}
    const sources: Record<string, number> = {}
    for (const layer of [...layers].sort((a, b) => a.provider - b.provider)) {
        for (const [key, val] of Object.entries(layer.data)) {
            if (val != null) {
                values[key] = val
                sources[key] = layer.provider
            }
        }
    }
    return { values, sources }
})

const viewOptions = computed(() => {
    const layers = localLayers.value?.layers ?? []
    const options = [{ title: 'Merged', value: 'merged' }]
    for (const layer of layers) {
        options.push({ title: providerLabel(layer.provider), value: String(layer.provider) })
    }
    return options
})

const selectedViewLayer = computed(() => {
    if (selectedView.value === 'merged') return null
    return localLayers.value?.layers.find(l => l.provider === Number(selectedView.value)) ?? null
})

const isEditable = computed(() => selectedView.value === 'merged' || selectedView.value === '99')

const isDirty = computed(
    () => JSON.stringify(overridesLayer.value?.data ?? {}) !== serverSnapshot.value
)

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
    if (Array.isArray(val)) return val.join(', ')
    return String(val)
}

function hasOverride(key: keyof ContentMetadata): boolean {
    const val = (overridesLayer.value?.data as any)?.[key]
    return val != null
}

function openFieldEditor(key: keyof ContentMetadata) {
    editingField.value = key
    const val = (overridesLayer.value?.data as any)?.[key]
    editValue.value = val != null ? formatValue(val) : ''
}

function confirmFieldEdit() {
    if (!editingField.value || !overridesLayer.value) return
    const data = overridesLayer.value.data as Record<string, any>
    if (editValue.value === '') {
        delete data[editingField.value]
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
            if (field.key === 'authors') {
                payload[field.key] = String(val)
                    .split(',')
                    .map((s: string) => s.trim())
                    .filter(Boolean)
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
import { Modals } from '@/utils/modals'
import Self from './EditMetadataModal.vue'

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
