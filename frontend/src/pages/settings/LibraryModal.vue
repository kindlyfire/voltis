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
            <fieldset ref="sourcesEl" class="flex flex-col gap-2">
                <legend class="mb-2 text-sm font-semibold">Sources</legend>
                <div
                    v-for="(source, index) in form.values.value.sources"
                    :key="index"
                    class="flex items-center gap-1"
                >
                    <ATextField
                        :model-value="source.path_uri"
                        :label="`Source ${index + 1}`"
                        placeholder="/path/to/folder"
                        size="sm"
                        class="flex-1"
                        @update:model-value="(v: string) => updateSource(index, v)"
                    />
                    <AIconButton
                        :icon="IconClose"
                        :label="`Remove source ${index + 1}`"
                        size="sm"
                        @click="removeSource(index)"
                    />
                </div>
                <div>
                    <AButton
                        ref="addSourceButton"
                        variant="tonal"
                        size="sm"
                        :leading-icon="IconPlus"
                        @click="addSource"
                    >
                        Add source
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
import { computed, nextTick, useId, useTemplateRef, watch } from 'vue'
import { z } from 'zod'
import { showConfirmModal } from '@/components/ConfirmModal.vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import AIconButton from '@/ui/AIconButton.vue'
import ASelect from '@/ui/ASelect.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconClose, IconPlus } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { librariesApi } from '@/utils/api/libraries'
import { useForm } from '@/utils/forms'

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
    }),
    initialValues: {
        name: '',
        type: null,
        sources: [],
    },
    onSubmit: async values => {
        await upsert.mutateAsync({
            id: isNew.value ? undefined : props.libraryId,
            name: values.name,
            type: values.type!,
            sources: values.sources.filter(s => s.path_uri.trim() !== ''),
        })
        toast.show({ message: isNew.value ? `Created ${values.name}` : 'Library saved' })
        props.close()
    },
})

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

watch(
    () => library.value,
    l => {
        if (l && !isNew.value) {
            form.setValues({ name: l.name, type: l.type, sources: l.sources })
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
