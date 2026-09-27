<template>
    <ADialog
        :open="open"
        :title="isNew ? 'Create library' : 'Edit library'"
        @update:open="v => !v && close()"
    >
        <form :id="formId" novalidate class="flex flex-col gap-3" @submit="form.onSubmit">
            <ATextField v-bind="form.field('name')" label="Name" autocomplete="off" autofocus />
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
                @update:model-value="v => v && form.setValue('book_series_inference', v)"
            />
            <ASwitch
                v-bind="form.field('auto_match')"
                label="Match series automatically"
                description="Links series to metadata providers in the background with MangaBaka."
            />
            <fieldset ref="sourcesEl" class="flex flex-col gap-2">
                <legend class="mb-2 text-sm font-semibold">Sources</legend>
                <div
                    v-for="(source, index) in form.values.value.sources"
                    :key="index"
                    class="flex items-start gap-1"
                >
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
                    <AButton variant="text" size="sm" :leading-icon="IconPlus" @click="addSource">
                        Add path manually
                    </AButton>
                </div>
            </fieldset>
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
import { computed, nextTick, useId, useTemplateRef, watch } from 'vue'
import { z } from 'zod'
import { showConfirmModal } from '@/components/ConfirmModal.vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import AIconButton from '@/ui/AIconButton.vue'
import ASelect from '@/ui/ASelect.vue'
import ASwitch from '@/ui/ASwitch.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconClose, IconFolderOpen, IconPlus } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { librariesApi } from '@/utils/api/libraries'
import { useForm } from '@/utils/forms'
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
                path_uri: z.string(),
            })
        ),
        book_series_inference: z.enum(['off', 'conservative']),
        auto_match: z.boolean(),
    }),
    initialValues: {
        name: '',
        type: null,
        sources: [],
        book_series_inference: 'conservative',
        auto_match: false,
    },
    onSubmit: async values => {
        await upsert.mutateAsync({
            id: isNew.value ? undefined : props.libraryId,
            name: values.name,
            type: values.type!,
            sources: values.sources.filter(s => s.path_uri.trim() !== ''),
            settings: {
                book_series_inference: values.book_series_inference,
                auto_match: values.auto_match,
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

function sourceInputs() {
    return [...(sourcesEl.value?.querySelectorAll('input') ?? [])]
}

async function addSource() {
    form.setValue('sources', [...form.values.value.sources, { path_uri: '' }])
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
    sources[index] = { path_uri: value }
    form.setValue('sources', sources)
}

const sourcePaths = computed(() => form.values.value.sources.map(s => s.path_uri.trim()))
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
    form.setValue('sources', [...form.values.value.sources, ...added.map(p => ({ path_uri: p }))])
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
            form.setValues({
                name: l.name,
                type: l.type,
                sources: l.sources,
                book_series_inference: l.settings.book_series_inference,
                auto_match: l.settings.auto_match,
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
