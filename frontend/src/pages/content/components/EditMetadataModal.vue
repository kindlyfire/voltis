<template>
    <ADialog :open="open" title="Edit metadata" size="lg" @update:open="v => !v && close()">
        <div class="flex flex-col gap-5">
            <QueryError :query="qView" />
            <QueryError :query="qConfig" />

            <div v-if="!view || !qConfig.data.value" class="flex justify-center py-10">
                <ASpinner />
            </div>

            <template v-else>
                <div class="flex flex-wrap items-center gap-2">
                    <ASelect
                        ref="viewSelect"
                        v-model="selectedView"
                        :options="viewOptions"
                        label="Source"
                        class="w-48"
                    />
                    <AButton
                        v-if="selectedLayer?.kind === 'provider'"
                        variant="tonal"
                        :leading-icon="IconCodeJson"
                        @click="showRawDialog = true"
                    >
                        Raw data
                    </AButton>
                </div>

                <dl v-if="rows.length" class="flex flex-col">
                    <div
                        v-for="row in rows"
                        :key="row.def.key"
                        class="border-outline-variant flex items-start gap-x-4 border-t py-1.5 pl-1 max-sm:flex-wrap"
                    >
                        <dt class="flex min-h-8 w-40 shrink-0 flex-wrap items-center gap-1.5">
                            {{ row.def.label }}
                            <AChip
                                v-for="chip in row.chips"
                                :key="chip"
                                size="sm"
                                :tone="
                                    chip === 'Edited' || chip === 'Overrides'
                                        ? 'primary'
                                        : 'neutral'
                                "
                            >
                                {{ chip }}
                            </AChip>
                        </dt>
                        <dd
                            class="text-fg-muted min-w-0 flex-1 py-1.5 [overflow-wrap:anywhere] whitespace-pre-wrap max-sm:order-last max-sm:basis-full max-sm:pt-0"
                        >
                            {{
                                row.value !== null
                                    ? formatValue(row.value)
                                    : row.def.type === 'cover'
                                      ? 'Local cover'
                                      : 'Cleared'
                            }}
                        </dd>
                        <div v-if="editing && row.def.editable" class="flex max-sm:ml-auto">
                            <AIconButton
                                v-if="row.def.type !== 'cover'"
                                :id="`${idPrefix}-${row.def.key}`"
                                :icon="IconPencil"
                                :label="`Edit ${row.def.label}`"
                                size="sm"
                                @click="openEditor(row.def)"
                            />
                            <AIconButton
                                v-if="row.value !== null"
                                :icon="IconClose"
                                :label="
                                    row.def.type === 'cover'
                                        ? 'Use the local cover'
                                        : `Clear ${row.def.label}`
                                "
                                size="sm"
                                @click="edits.set(row.def.key, null)"
                            />
                            <AIconButton
                                v-if="overridden(row.def.key)"
                                :icon="IconRestart"
                                :label="`Reset ${row.def.label}`"
                                size="sm"
                                @click="reset(row.def.key)"
                            />
                        </div>
                    </div>
                </dl>
                <p v-else class="text-fg-muted">No metadata from this source.</p>

                <section v-if="editing && addable.length" class="flex flex-col gap-2">
                    <h3 class="text-fg-muted text-xs font-semibold tracking-wide">Add a field</h3>
                    <div class="flex flex-wrap gap-1.5">
                        <AChip
                            v-for="def in addable"
                            :key="def.key"
                            variant="outlined"
                            size="sm"
                            :leading-icon="IconPlus"
                            @click="openEditor(def)"
                        >
                            {{ def.label }}
                        </AChip>
                    </div>
                </section>

                <section v-if="view.links.length" class="flex flex-col gap-2">
                    <h3 class="text-fg-muted text-xs font-semibold tracking-wide">Sources</h3>
                    <ProviderLinkCard
                        v-for="link in view.links"
                        :key="link.provider"
                        :content-id="contentId"
                        :link="link"
                        :title="localTitle"
                    />
                </section>
            </template>

            <QueryError :mutation="mSave" />
        </div>

        <template #actions>
            <AButton variant="text" tone="neutral" @click="close()">Close</AButton>
            <AButton
                :disabled="!edits.size"
                focusable-when-disabled
                :loading="mSave.isPending.value"
                @click="save()"
            >
                Save
            </AButton>
        </template>

        <!-- Nested here so they stack on this dialog and return focus to it. -->
        <MetadataFieldDialog
            v-model:open="editorOpen"
            :def="editorField?.def"
            :value="editorField?.value"
            @confirm="confirmEdit"
        />

        <ADialog
            v-model:open="showRawDialog"
            :title="`Raw data: ${selectedLayer?.label ?? ''}`"
            size="xl"
        >
            <div v-if="selectedLayer" class="flex flex-col gap-4 lg:flex-row">
                <section class="flex min-w-0 flex-1 flex-col gap-1">
                    <h3 class="text-fg-muted text-xs font-semibold tracking-wide">
                        Normalized data
                    </h3>
                    <pre class="raw-json">{{ JSON.stringify(selectedLayer.fields, null, 2) }}</pre>
                </section>
                <section class="flex min-w-0 flex-1 flex-col gap-1">
                    <h3 class="text-fg-muted text-xs font-semibold tracking-wide">Raw response</h3>
                    <pre class="raw-json">{{ JSON.stringify(selectedLayer.raw, null, 2) }}</pre>
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
import { computed, nextTick, reactive, ref, useId, useTemplateRef } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import AChip from '@/ui/AChip.vue'
import ADialog from '@/ui/ADialog.vue'
import AIconButton from '@/ui/AIconButton.vue'
import ASelect from '@/ui/ASelect.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { IconClose, IconCodeJson, IconPencil, IconPlus, IconRestart } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { metadataApi, ownTitle, type FieldDef, type MetadataValues } from '@/utils/api/metadata'
import { RequestError } from '@/utils/fetch'
import MetadataFieldDialog from './MetadataFieldDialog.vue'
import ProviderLinkCard from './ProviderLinkCard.vue'

const props = defineProps<{
    open: boolean
    close: () => void
    contentId: string
}>()

const toast = useToast()
const idPrefix = useId()
const viewSelect = useTemplateRef('viewSelect')
const qConfig = metadataApi.useConfig()
const qView = metadataApi.useContent(() => props.contentId)
const mSave = metadataApi.useSaveOverrides()

const view = computed(() => qView.data.value)

/** Unsaved changes to the overrides: a value, `null` to clear, or RESET to drop the override.
 * Kept apart from the server's layer, so refetches and conflicts keep them. */
const RESET = Symbol('reset')
const edits = reactive(new Map<string, unknown>())

const serverOverrides = computed(
    () => view.value?.layers.find(l => l.kind === 'overrides')?.fields ?? {}
)

const localTitle = computed(() => (view.value ? ownTitle(view.value) : ''))

/** Every field of this content type; the others' overrides are kept as they are. */
const defs = computed(() => {
    const type = view.value?.type
    return (qConfig.data.value?.fields ?? []).filter(f => type && f.content_types.includes(type))
})

const selectedView = ref('merged')
const viewOptions = computed(() => [
    { label: 'Merged', value: 'merged' },
    ...(view.value?.layers ?? []).map(l => ({ label: l.label, value: l.source })),
])
const selectedLayer = computed(() => view.value?.layers.find(l => l.source === selectedView.value))
const editing = computed(() => selectedView.value === 'merged')
const showRawDialog = ref(false)

/** The override that would be saved for a key, or RESET for none. */
function override(key: string): unknown {
    if (edits.has(key)) return edits.get(key)
    return key in serverOverrides.value ? serverOverrides.value[key] : RESET
}

function overridden(key: string) {
    return override(key) !== RESET
}

/** What the field would show once saved; `null` when cleared. */
function effective(key: string): unknown {
    const o = override(key)
    if (o !== RESET) return o
    if (edits.has(key)) {
        // A reset shows the value from below the overrides.
        const below = view.value!.layers.filter(
            l => l.kind !== 'overrides' && l.fields[key] != null
        )
        return below.at(-1)?.fields[key]
    }
    return view.value!.merged[key]
}

const rows = computed(() => {
    if (!view.value) return []
    if (!editing.value) {
        const fields = selectedLayer.value?.fields ?? {}
        return (qConfig.data.value?.fields ?? [])
            .filter(def => def.key in fields)
            .map(def => ({ def, value: fields[def.key], chips: [] as string[] }))
    }
    const labels = new Map(view.value.layers.map(l => [l.source, l.label]))
    return defs.value.flatMap(def => {
        const value = effective(def.key)
        if (value === undefined) return []
        const chips = edits.has(def.key)
            ? ['Edited']
            : (view.value!.sources[def.key] ?? []).map(s => labels.get(s) ?? s)
        return [{ def, value, chips }]
    })
})

const addable = computed(() =>
    defs.value
        .filter(def => def.editable && def.type !== 'cover' && effective(def.key) === undefined)
        .sort((a, b) => a.label.localeCompare(b.label))
)

function formatValue(value: unknown): string {
    if (!Array.isArray(value))
        return typeof value === 'object' ? formatValue([value]) : String(value)
    return value
        .map(v =>
            typeof v !== 'object' ? v : 'role' in v ? `${v.name} (${v.role})` : (v.label ?? v.url)
        )
        .join(', ')
}

function reset(key: string) {
    if (key in serverOverrides.value) edits.set(key, RESET)
    else edits.delete(key)
}

const editorOpen = ref(false)
/** The field being edited and the value its draft starts from. */
const editorField = ref<{ def: FieldDef; value: unknown }>()

function openEditor(def: FieldDef) {
    editorField.value = { def, value: effective(def.key) }
    editorOpen.value = true
}

async function confirmEdit(value: unknown) {
    const key = editorField.value!.def.key
    edits.set(key, value)
    // The field's own button, or the Source select when it was added from a chip.
    await nextTick()
    const button = document.getElementById(`${idPrefix}-${key}`)
    if (button) button.focus()
    else viewSelect.value?.focus()
}

function save() {
    const fields: MetadataValues = { ...serverOverrides.value }
    for (const [key, value] of edits) {
        if (value === RESET) delete fields[key]
        else fields[key] = value
    }
    mSave.mutate(
        { contentId: props.contentId, rev: view.value!.overrides_rev, fields },
        {
            onSuccess() {
                edits.clear()
                toast.show({ message: 'Saved the metadata' })
            },
            onError(err) {
                // Someone saved first: show their values, keeping these edits to save over them.
                if (err instanceof RequestError && err.response?.status === 409) qView.refetch()
            },
        }
    )
}
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './EditMetadataModal.vue'

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
