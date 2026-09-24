<template>
    <ADialog
        :open="open"
        :title="isNew ? 'Create list' : 'Edit list'"
        @update:open="v => !v && close()"
    >
        <form :id="formId" novalidate class="flex flex-col gap-4" @submit="form.onSubmit">
            <ATextField v-bind="form.field('name')" label="Name" autofocus />
            <ATextField
                multiline
                v-bind="form.field('description')"
                label="Description"
                auto-grow
            />
            <ASelect
                :model-value="form.values.value.visibility"
                :options="visibilityOptions"
                label="Visibility"
                @update:model-value="v => v && form.setValue('visibility', v)"
            />
            <QueryError :mutation="form.mutation" />
            <QueryError :mutation="deleteList" />
        </form>
        <template #actions>
            <AButton
                v-if="!isNew"
                variant="text"
                tone="danger"
                class="mr-auto"
                :loading="deleteList.isPending.value"
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
import { computed, useId, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { z } from 'zod'
import { showConfirmModal } from '@/components/ConfirmModal.vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import ASelect from '@/ui/ASelect.vue'
import ATextField from '@/ui/ATextField.vue'
import { useToast } from '@/ui/useToast'
import { customListsApi } from '@/utils/api/custom-lists'
import { useForm } from '@/utils/forms'

const props = defineProps<{
    open: boolean
    close: () => void
    listId: string
}>()

const isNew = computed(() => props.listId === 'new')
const visibilityOptions = [
    { label: 'Public', value: 'public' },
    { label: 'Private', value: 'private' },
    { label: 'Unlisted', value: 'unlisted' },
] as const
const formId = useId()
const toast = useToast()
const route = useRoute()
const router = useRouter()

const list = customListsApi.useGet(() => (isNew.value ? null : props.listId), {
    enabled: computed(() => !isNew.value),
})
const createList = customListsApi.useCreate()
const updateList = customListsApi.useUpdate()
const deleteList = customListsApi.useDelete()

const form = useForm({
    schema: z.object({
        name: z.string().trim().min(1, 'Name is required'),
        description: z.string().optional(),
        visibility: z.enum(['public', 'private', 'unlisted']),
    }),
    initialValues: {
        name: '',
        description: '',
        visibility: 'private',
    },
    onSubmit: async values => {
        if (isNew.value) {
            await createList.mutateAsync(values)
            toast.show({ message: `Created ${values.name}` })
        } else {
            await updateList.mutateAsync({ id: props.listId, ...values })
            toast.show({ message: 'List saved' })
        }
        props.close()
    },
})

watch(
    () => list.data?.value,
    val => {
        if (val && !isNew.value) {
            form.setValues({
                name: val.name,
                description: val.description ?? '',
                visibility: val.visibility,
            })
        }
    },
    { immediate: true }
)

async function handleDelete() {
    if (isNew.value) return
    const name = list.data.value?.name ?? 'this list'
    const confirmed = await showConfirmModal({
        title: 'Delete list?',
        message: `${name} and its entries will be deleted. The series and books stay in your libraries.`,
        confirmText: 'Delete',
        tone: 'danger',
    })
    if (!confirmed) return
    await deleteList.mutateAsync(props.listId)
    toast.show({ message: `Deleted ${name}` })
    props.close()
    if (route.params.id === props.listId) router.push('/lists')
}
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './ListModal.vue'

export function showListModal(listId: string): Promise<void> {
    return Modals.show(Self, { listId })
}
</script>
