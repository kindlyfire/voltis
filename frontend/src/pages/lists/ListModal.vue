<template>
	<VDialog
		:model-value="modelValue"
		@update:model-value="$emit('update:modelValue', $event)"
		max-width="500"
	>
		<VCard>
			<VCardTitle>{{ isNew ? 'Create List' : 'Edit List' }}</VCardTitle>
			<VCardText>
				<VForm @submit="form.onSubmit" class="space-y-4!">
					<AInput :input="form.getInputProps('name')" label="Name" autofocus />
					<AInput
						:input="form.getInputProps('description')"
						label="Description"
						type="textarea"
						auto-grow
					/>
					<VSelect
						:model-value="form.values.value.visibility"
						@update:model-value="form.setValue('visibility', $event)"
						label="Visibility"
						:items="visibilityOptions"
						hide-details
					/>

					<AMutationError :mutation="form.mutation" />

					<div class="flex gap-2">
						<VBtn
							type="submit"
							color="primary"
							:loading="form.mutation.isPending.value"
						>
							{{ isNew ? 'Create' : 'Update' }}
						</VBtn>
						<VBtn variant="text" @click="$emit('update:modelValue', false)"
							>Cancel</VBtn
						>
						<VSpacer />
						<VBtn
							v-if="!isNew"
							color="error"
							variant="text"
							:loading="deleteList.isPending.value"
							@click="handleDelete"
						>
							Delete
						</VBtn>
					</div>
				</VForm>
			</VCardText>
		</VCard>
	</VDialog>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { z } from 'zod'
import AInput from '@/components/AInput.vue'
import AMutationError from '@/components/AMutationError.vue'
import { useForm } from '@/utils/forms'
import { customListsApi } from '@/utils/api/custom-lists'

const props = defineProps<{
	listId: string | null
	modelValue: boolean
}>()

const emit = defineEmits<{
	'update:modelValue': [boolean]
}>()

const isNew = computed(() => props.listId === 'new')
const visibilityOptions = ['public', 'private', 'unlisted']

const list = customListsApi.useGet(() => (isNew.value ? null : props.listId), {
	enabled: computed(() => !isNew.value && !!props.listId),
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
		} else {
			await updateList.mutateAsync({ id: props.listId!, ...values })
		}
		emit('update:modelValue', false)
	},
})

watch(
	() => props.listId,
	() => {
		form.reset()
		if (isNew.value) {
			form.setValues({ name: '', description: '', visibility: 'private' })
		} else if (list.data?.value) {
			form.setValues({
				name: list.data.value.name,
				description: list.data.value.description ?? '',
				visibility: list.data.value.visibility,
			})
		}
	},
	{ immediate: true }
)

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
	}
)

async function handleDelete() {
	if (!props.listId || isNew.value) return
	await deleteList.mutateAsync(props.listId)
	emit('update:modelValue', false)
}
</script>
