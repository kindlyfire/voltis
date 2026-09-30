<template>
    <ADialog
        :open="open"
        :title="isNew ? 'Create library' : 'Edit library'"
        size="lg"
        flex-body
        @update:open="v => !v && close()"
    >
        <form :id="formId" novalidate class="flex min-h-0 flex-1 flex-col gap-3" @submit="onSubmit">
            <ATabs v-model="tab" :options="tabOptions" label="Library settings" class="min-h-80">
                <template #general>
                    <div class="flex flex-col gap-3 pt-4">
                        <ATextField
                            v-bind="form.field('name')"
                            label="Name"
                            autocomplete="off"
                            autofocus
                        />
                        <ASelect
                            :model-value="form.values.value.type"
                            :options="typeOptions"
                            label="Type"
                            :readonly="!isNew"
                            :hint="isNew ? undefined : 'Fixed once the library is created.'"
                            :error="form.field('type').error"
                            @update:model-value="form.field('type')['onUpdate:modelValue']"
                        />
                        <ASelect
                            v-if="form.values.value.type === 'books'"
                            :model-value="form.values.value.book_series_inference"
                            :options="inferenceOptions"
                            label="Series without metadata"
                            :hint="inferenceHint"
                            @update:model-value="
                                v => v && form.setValue('book_series_inference', v)
                            "
                        />
                        <ASwitch
                            v-bind="form.field('always_remove_missing')"
                            label="Remove missing items without checking"
                            description="Normally a scan removes nothing when a source folder is empty or most of it is missing, as when a drive is not mounted. Applies to scans queued after saving."
                        />
                        <div class="mt-3 flex flex-col gap-3" :aria-busy="config.isPending.value">
                            <QueryError :query="config" />
                            <template v-if="config.data.value">
                                <div
                                    v-for="p in providers"
                                    :key="p.name"
                                    class="flex flex-col items-start"
                                >
                                    <ASwitch
                                        :model-value="!!form.values.value.auto_match[p.name]"
                                        :label="`Match series automatically with ${p.label}`"
                                        description="Links series to the provider in the background."
                                        class="self-stretch"
                                        @update:model-value="v => setLibraryAutoMatch(p.name, v)"
                                    />
                                    <AButton
                                        v-if="overrideCount(form.values.value.sources, p.name)"
                                        variant="text"
                                        size="sm"
                                        @click="showOverrides(p.name)"
                                    >
                                        Overridden in
                                        {{
                                            plural(
                                                overrideCount(form.values.value.sources, p.name),
                                                'source'
                                            )
                                        }}
                                    </AButton>
                                </div>
                                <p v-if="!providers.length" class="text-fg-muted">
                                    No metadata provider handles this library's type.
                                </p>
                            </template>
                            <ASkeleton v-else-if="config.isPending.value" height="56px" />
                        </div>
                    </div>
                </template>
                <template #tab-sources>
                    Sources
                    <AIcon
                        v-if="!hasSources"
                        :icon="IconAlert"
                        label="No sources"
                        class="text-warning"
                    />
                </template>
                <template #sources>
                    <fieldset ref="sourcesEl" class="flex flex-col gap-3 pt-4">
                        <legend class="sr-only">Sources</legend>
                        <p v-if="form.values.value.sources.length" class="text-fg-muted">
                            Click
                            <AIcon :icon="IconTune" class="inline align-[-3px] text-base" />
                            to override the library's settings for a source.
                        </p>
                        <div
                            v-for="(source, index) in form.values.value.sources"
                            :key="source.key"
                            class="flex flex-col gap-2"
                        >
                            <div class="flex items-start gap-1">
                                <ATextField
                                    :model-value="source.path_uri"
                                    :label="`Source ${index + 1}`"
                                    placeholder="/path/to/folder"
                                    size="sm"
                                    :hint="sourceHint(index)"
                                    hint-tone="warning"
                                    class="flex-1"
                                    @update:model-value="(v: string) => updateSource(index, v)"
                                />
                                <!-- Centred on the 40px field box, not on the box plus its hint. -->
                                <div class="flex h-10 items-center gap-1">
                                    <AIconButton
                                        :icon="IconTune"
                                        :label="`Settings of source ${index + 1}`"
                                        size="sm"
                                        :variant="overrides(source).length ? 'tonal' : 'standard'"
                                        :tone="overrides(source).length ? 'primary' : 'neutral'"
                                        :aria-expanded="expanded.has(source.key)"
                                        :aria-controls="`${formId}-${source.key}`"
                                        @click="toggleExpanded(source.key)"
                                    />
                                    <AIconButton
                                        :icon="IconFolderOpen"
                                        :label="`Browse for source ${index + 1}`"
                                        size="sm"
                                        @click="browseSource(index)"
                                    />
                                    <AIconButton
                                        :icon="IconClose"
                                        :label="`Remove source ${index + 1}`"
                                        size="sm"
                                        @click="removeSource(index)"
                                    />
                                </div>
                            </div>
                            <div
                                v-if="!expanded.has(source.key) && overrides(source).length"
                                class="flex flex-wrap gap-1"
                            >
                                <AChip v-for="o in overrides(source)" :key="o" size="sm">
                                    {{ o }}
                                </AChip>
                            </div>
                            <div
                                v-if="expanded.has(source.key)"
                                :id="`${formId}-${source.key}`"
                                class="border-outline-variant ml-3 flex flex-col gap-3 border-l pl-3"
                            >
                                <div
                                    v-for="p in providers"
                                    :key="p.name"
                                    class="flex flex-col gap-1"
                                >
                                    <span class="text-sm font-semibold">
                                        Match series automatically with {{ p.label }}
                                    </span>
                                    <ASegmented
                                        size="sm"
                                        :model-value="overrideOf(source, p.name)"
                                        :options="overrideOptions(p.name)"
                                        :label="`${p.label} matching for source ${index + 1}`"
                                        @update:model-value="v => setOverride(index, p.name, v)"
                                    />
                                    <p
                                        v-if="
                                            !autoMatchOn(form.values.value, source.settings, p.name)
                                        "
                                        class="text-fg-muted text-xs"
                                    >
                                        Series that also have files in a source that matches
                                        automatically are still matched.
                                    </p>
                                </div>
                                <p
                                    v-if="config.data.value && !providers.length"
                                    class="text-fg-muted text-sm"
                                >
                                    No metadata provider handles this library's type.
                                </p>
                            </div>
                        </div>
                        <div class="flex flex-wrap gap-2">
                            <AButton
                                ref="addSourceButton"
                                variant="tonal"
                                size="sm"
                                :leading-icon="IconFolderOpen"
                                @click="browseSources"
                            >
                                Browse folders…
                            </AButton>
                            <AButton
                                variant="text"
                                size="sm"
                                :leading-icon="IconPlus"
                                @click="addSource"
                            >
                                Add path manually
                            </AButton>
                        </div>
                    </fieldset>
                </template>
            </ATabs>
            <QueryError :mutation="form.mutation" />
            <QueryError :mutation="deleteLibrary" />
        </form>
        <template #actions>
            <AButton
                v-if="!isNew"
                variant="text"
                tone="danger"
                class="mr-auto"
                :loading="deleteLibrary.isPending.value"
                @click="handleDelete"
            >
                Delete
            </AButton>
            <AButton variant="text" tone="neutral" @click="close()">Cancel</AButton>
            <AButton type="submit" :form="formId" :loading="form.mutation.isPending.value">
                {{ isNew ? 'Create' : 'Save' }}
            </AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { refDebounced } from '@vueuse/core'
import { computed, nextTick, reactive, ref, toRaw, useId, useTemplateRef, watch } from 'vue'
import { z } from 'zod'
import { showConfirmModal } from '@/components/ConfirmModal.vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import AChip from '@/ui/AChip.vue'
import ADialog from '@/ui/ADialog.vue'
import AIcon from '@/ui/AIcon.vue'
import AIconButton from '@/ui/AIconButton.vue'
import ASegmented from '@/ui/ASegmented.vue'
import ASelect from '@/ui/ASelect.vue'
import ASkeleton from '@/ui/ASkeleton.vue'
import ASwitch from '@/ui/ASwitch.vue'
import ATabs from '@/ui/ATabs.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconAlert, IconClose, IconFolderOpen, IconPlus, IconTune } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { librariesApi } from '@/utils/api/libraries'
import { metadataApi } from '@/utils/api/metadata'
import { useForm } from '@/utils/forms'
import { autoMatchOn, overrideCount } from '@/utils/librarySettings'
import { plural } from '@/utils/misc'
import { showFolderPicker } from './FolderPickerModal.vue'
import { useSourceOverlaps } from './useSourceOverlaps'

const props = defineProps<{
    open: boolean
    close: () => void
    libraryId: string
}>()

const isNew = computed(() => props.libraryId === 'new')
const libraries = librariesApi.useList()
const library = computed(() => libraries.data?.value?.find(l => l.id === props.libraryId))
const upsert = librariesApi.useUpsert()
const deleteLibrary = librariesApi.useDelete()
const formId = useId()
const toast = useToast()
const typeOptions = [
    { value: 'comics', label: 'Comics' },
    { value: 'books', label: 'Books' },
] as const
const inferenceOptions = [
    { value: 'conservative', label: 'Group by volume number in title or filename' },
    { value: 'off', label: 'Keep as standalone books' },
] as const
const tabOptions = [
    { value: 'general', label: 'General' },
    { value: 'sources', label: 'Sources' },
] as const
type Tab = (typeof tabOptions)[number]['value']
const tab = ref<Tab>('general')
const sourcesEl = useTemplateRef('sourcesEl')
const addSourceButton = useTemplateRef<{ $el: HTMLElement }>('addSourceButton')

const form = useForm({
    schema: z.object({
        name: z.string().trim().min(1, 'Name is required'),
        type: z
            .enum(['comics', 'books'])
            .nullable()
            .refine(val => val !== null, 'Type is required'),
        sources: z.array(
            z.object({
                key: z.string(), // local, to key the rows
                path_uri: z.string(),
                settings: z.object({ auto_match: z.record(z.string(), z.boolean()).optional() }),
            })
        ),
        book_series_inference: z.enum(['off', 'conservative']),
        auto_match: z.record(z.string(), z.boolean()),
        always_remove_missing: z.boolean(),
    }),
    initialValues: {
        name: '',
        type: null,
        sources: [],
        book_series_inference: 'conservative',
        auto_match: {},
        always_remove_missing: false,
    },
    onSubmit: async values => {
        await upsert.mutateAsync({
            id: isNew.value ? undefined : props.libraryId,
            name: values.name,
            type: values.type!,
            sources: values.sources
                .filter(s => s.path_uri.trim() !== '')
                .map(({ path_uri, settings }) => ({ path_uri, settings })),
            settings: {
                book_series_inference: values.book_series_inference,
                auto_match: values.auto_match,
                always_remove_missing: values.always_remove_missing,
            },
        })
        toast.show({ message: isNew.value ? `Created ${values.name}` : 'Library saved' })
        props.close()
    },
})

const inferenceHint = computed(() =>
    library.value &&
    library.value.settings.book_series_inference !== form.values.value.book_series_inference
        ? 'Existing books are regrouped on the next forced scan.'
        : undefined
)

// A field that fails validation may sit in a hidden tab: show it, so the form can focus it.
function onSubmit(e: Event) {
    form.onSubmit(e)
    const field = form.errors.value[0]?.path[0]
    if (field !== undefined) tab.value = field === 'sources' ? 'sources' : 'general'
}

const config = metadataApi.useConfig()
const seriesTypes = { comics: 'comic_series', books: 'book_series' } as const
// The providers that match this type of library; all of them until a type is picked.
const providers = computed(() => {
    const type = form.values.value.type
    return (config.data.value?.providers ?? []).filter(
        p => !type || p.content_types.includes(seriesTypes[type])
    )
})

type SourceRow = (typeof form.values.value.sources)[number]
type Override = 'inherit' | 'on' | 'off'

let lastKey = 0
const newRow = (path_uri: string, settings: SourceRow['settings'] = {}): SourceRow => ({
    key: String(++lastKey),
    path_uri,
    settings,
})

const expanded = reactive(new Set<string>())

function toggleExpanded(key: string) {
    if (!expanded.delete(key)) expanded.add(key)
}

function setLibraryAutoMatch(provider: string, on: boolean) {
    form.setValue('auto_match', { ...form.values.value.auto_match, [provider]: on })
}

function overrideOf(source: SourceRow, provider: string): Override {
    const v = source.settings.auto_match?.[provider]
    return v === undefined ? 'inherit' : v ? 'on' : 'off'
}

function overrideOptions(provider: string) {
    const inherited = form.values.value.auto_match[provider] ? 'on' : 'off'
    return [
        { value: 'inherit', label: `Inherit (${inherited})` },
        { value: 'on', label: 'On' },
        { value: 'off', label: 'Off' },
    ] as const
}

// Immutable, so no edit reaches the libraries query's data the form was loaded from.
function setOverride(index: number, provider: string, value: Override) {
    const sources = [...form.values.value.sources]
    const row = sources[index]!
    const auto_match = { ...row.settings.auto_match }
    if (value === 'inherit') delete auto_match[provider]
    else auto_match[provider] = value === 'on'
    sources[index] = { ...row, settings: { ...row.settings, auto_match } }
    form.setValue('sources', sources)
}

// Of the providers shown; keys of others stay in the form, unseen.
function overrides(source: SourceRow) {
    return providers.value.flatMap(p => {
        const on = source.settings.auto_match?.[p.name]
        return on === undefined ? [] : [`${p.label}: ${on ? 'on' : 'off'}`]
    })
}

function showOverrides(provider: string) {
    for (const s of form.values.value.sources) {
        if (s.settings.auto_match?.[provider] !== undefined) expanded.add(s.key)
    }
    tab.value = 'sources'
}

function sourceInputs() {
    return [...(sourcesEl.value?.querySelectorAll('input') ?? [])]
}

async function addSource() {
    form.setValue('sources', [...form.values.value.sources, newRow('')])
    await nextTick()
    sourceInputs().at(-1)?.focus()
}

// Focus moves to the source that took the removed one's place, else to the one before it.
async function removeSource(index: number) {
    form.setValue(
        'sources',
        form.values.value.sources.filter((_, i) => i !== index)
    )
    await nextTick()
    const inputs = sourceInputs()
    const next = inputs[Math.min(index, inputs.length - 1)]
    if (next) next.focus()
    else addSourceButton.value?.$el.focus()
}

function updateSource(index: number, value: string) {
    const sources = [...form.values.value.sources]
    sources[index] = { ...sources[index]!, path_uri: value }
    form.setValue('sources', sources)
}

const sourcePaths = computed(() => form.values.value.sources.map(s => s.path_uri.trim()))
const hasSources = computed(() => sourcePaths.value.some(p => p))
const otherPaths = (except?: number) => sourcePaths.value.filter((p, i) => p && i !== except)
const excludeLibraryId = isNew.value ? undefined : props.libraryId
const overlaps = useSourceOverlaps(excludeLibraryId)
const settledPaths = refDebounced(sourcePaths, 400)
watch(settledPaths, paths => overlaps.resolve(paths), { immediate: true })

async function browseSource(index: number) {
    const paths = await showFolderPicker({
        initialPath: sourcePaths.value[index] || undefined,
        excludeLibraryId,
        otherSourcePaths: otherPaths(index),
    })
    if (paths?.[0]) updateSource(index, paths[0])
}

async function browseSources() {
    const paths = await showFolderPicker({
        multiple: true,
        excludeLibraryId,
        otherSourcePaths: otherPaths(),
    })
    if (!paths) return
    const added = paths.filter(p => !sourcePaths.value.includes(p))
    form.setValue('sources', [...form.values.value.sources, ...added.map(p => newRow(p))])
}

function sourceHint(index: number) {
    const path = sourcePaths.value[index]
    if (!path) return undefined
    const others = sourcePaths.value.flatMap((p, i) =>
        p && i !== index ? [{ path: p, label: `Source ${i + 1}` }] : []
    )
    return overlaps.rawWarning(path, others)?.long
}

// Once per modal: a background refetch (a scan finishing, say) must not clobber unsaved edits.
let initialised = false
watch(
    () => library.value,
    l => {
        if (l && !isNew.value && !initialised) {
            initialised = true
            // A copy: setValues keeps what it is given, and edits must not reach the query's data.
            const { name, type, sources, settings } = structuredClone(toRaw(l))
            form.setValues({
                name,
                type,
                sources: sources.map(s => newRow(s.path_uri, s.settings)),
                book_series_inference: settings.book_series_inference,
                auto_match: settings.auto_match,
                always_remove_missing: settings.always_remove_missing ?? false,
            })
        }
    },
    { immediate: true }
)

async function handleDelete() {
    if (isNew.value) return
    const name = library.value?.name ?? 'this library'
    const confirmed = await showConfirmModal({
        title: 'Delete library?',
        message: `${name} and all its content will be removed. The files on disk stay.`,
        confirmText: 'Delete',
        tone: 'danger',
    })
    if (!confirmed) return
    await deleteLibrary.mutateAsync(props.libraryId)
    toast.show({ message: `Deleted ${name}` })
    props.close()
}
</script>

<script lang="ts">
import { Modals, type ShowOptions } from '@/utils/modals'
import Self from './LibraryModal.vue'

export function showLibraryModal(libraryId: string, options?: ShowOptions): Promise<void> {
    return Modals.show(Self, { libraryId }, options)
}
</script>
