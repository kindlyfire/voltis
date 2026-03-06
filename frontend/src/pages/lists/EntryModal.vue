<template>
    <VDialog :model-value="open" @update:model-value="v => !v && close()" max-width="500">
        <VCard>
            <VCardTitle>Edit Notes</VCardTitle>
            <VCardText>
                <VForm @submit="form.onSubmit" class="space-y-4!">
                    <div v-if="title" class="text-body-2 text-medium-emphasis">
                        {{ title }}
                    </div>

                    <AInput
                        :input="form.getInputProps('notes')"
                        label="Notes"
                        type="textarea"
                        auto-grow
                        rows="4"
                    />

                    <AQueryError :mutation="form.mutation" />

                    <div class="flex gap-2">
                        <VBtn
                            type="submit"
                            color="primary"
                            :loading="form.mutation.isPending.value"
                        >
                            Save
                        </VBtn>
                        <VBtn variant="text" @click="close()">Cancel</VBtn>
                    </div>
                </VForm>
            </VCardText>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
import { z } from 'zod'
import AInput from '@/components/AInput.vue'
import AQueryError from '@/components/AQueryError.vue'
import { useForm } from '@/utils/forms'
import { customListsApi } from '@/utils/api/custom-lists'

const props = defineProps<{
    open: boolean
    close: () => void
    listId: string
    entryId: string
    title?: string
    notes?: string | null
}>()

const updateEntry = customListsApi.useUpdateEntry()

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
