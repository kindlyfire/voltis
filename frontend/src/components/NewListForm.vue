<template>
    <!-- Esc cancels the form rather than closing the dialog. -->
    <form
        novalidate
        class="flex flex-col gap-2"
        @submit="form.onSubmit"
        @keydown.esc.prevent="emit('close')"
    >
        <ATextField ref="nameField" v-bind="form.field('name')" label="New list name" />
        <div class="flex justify-end gap-2">
            <AButton
                variant="text"
                tone="neutral"
                aria-label="Cancel new list"
                @click="emit('close')"
            >
                Cancel
            </AButton>
            <AButton type="submit" variant="tonal" :loading="form.mutation.isPending.value">
                Create
            </AButton>
        </div>
        <QueryError :mutation="form.mutation" />
    </form>
</template>

<script setup lang="ts">
import { z } from 'zod'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ATextField from '@/ui/ATextField.vue'
import { customListsApi } from '@/utils/api/custom-lists'
import type { CustomListPartial } from '@/utils/api/types'
import { useForm } from '@/utils/forms'

const emit = defineEmits<{ created: [list: CustomListPartial]; close: [] }>()

const nameField = useTemplateRef('nameField')
const mCreate = customListsApi.useCreate()

const form = useForm({
    schema: z.object({ name: z.string().trim().min(1, 'Name is required') }),
    initialValues: { name: '' },
    onSubmit: async ({ name }) => {
        emit('created', await mCreate.mutateAsync({ name, visibility: 'private' }))
        emit('close')
    },
})

// `autofocus` can't be used: dialogs only apply it on open.
onMounted(() => nameField.value?.focus())
</script>

<script lang="ts">
import { nextTick, onMounted, ref, useTemplateRef } from 'vue'

/** State for a dialog's footer "New list" button (bind `:ref="newList.buttonRef"`), which the form
 * replaces while open. */
export function useNewListToggle() {
    const creating = ref(false)
    let button: HTMLElement | undefined
    return {
        creating,
        buttonRef: (c: unknown) => {
            button = (c as { $el?: HTMLElement } | null)?.$el
        },
        start: () => (creating.value = true),
        async stop() {
            creating.value = false
            await nextTick()
            button?.focus()
        },
    }
}
</script>
