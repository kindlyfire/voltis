<template>
    <ADialog :open="open" title="Edit notes" :description="title" @update:open="v => !v && close()">
        <form :id="formId" novalidate class="flex flex-col gap-4" @submit="form.onSubmit">
            <ATextField
                multiline
                v-bind="form.field('notes')"
                label="Notes"
                auto-grow
                :rows="4"
                autofocus
            />
            <QueryError :mutation="form.mutation" />
        </form>
        <template #actions>
            <AButton variant="text" tone="neutral" @click="close()">Cancel</AButton>
            <AButton type="submit" :form="formId" :loading="form.mutation.isPending.value">
                Save
            </AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { useId } from 'vue'
import { z } from 'zod'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import ATextField from '@/ui/ATextField.vue'
import { useToast } from '@/ui/useToast'
import { customListsApi } from '@/utils/api/custom-lists'
import { useForm } from '@/utils/forms'

const props = defineProps<{
    open: boolean
    close: () => void
    listId: string
    entryId: string
    title?: string
    notes?: string | null
}>()

const updateEntry = customListsApi.useUpdateEntry()
const toast = useToast()
const formId = useId()

const form = useForm({
    schema: z.object({
        notes: z.string(),
    }),
    initialValues: {
        notes: props.notes ?? '',
    },
    onSubmit: async values => {
        await updateEntry.mutateAsync({
            listId: props.listId,
            entryId: props.entryId,
            notes: values.notes.trim() ? values.notes : null,
        })
        toast.show({ message: 'Notes saved' })
        props.close()
    },
})
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './EntryModal.vue'

export function showEntryModal(props: {
    listId: string
    entryId: string
    title?: string
    notes?: string | null
}): Promise<void> {
    return Modals.show(Self, props)
}
</script>
