<template>
	<VDialog :model-value="!!libraryId" @update:model-value="$emit('close')" max-width="500">
		<VCard>
			<VCardTitle>{{ isNew ? 'Create Library' : 'Edit Library' }}</VCardTitle>
			<VCardText>
				<VForm @submit="form.onSubmit" class="space-y-4!">
					<AInput :input="form.getInputProps('name')" label="Name" />
					<VSelect
						v-bind="form.getInputProps('type')"
						label="Type"
						:items="[
							{ title: 'Comics', value: 'comics' },
							{ title: 'Books', value: 'books' },
						]"
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
						<VBtn variant="text" @click="$emit('close')">Cancel</VBtn>
						<VSpacer />
						<VBtn
							v-if="!isNew"
							color="error"
							variant="text"
							:loading="deleteLibrary.isPending.value"
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
import { useForm } from '@/utils/forms'
import { librariesApi } from '@/utils/api/libraries'
import AInput from '@/components/AInput.vue'
import AMutationError from '@/components/AMutationError.vue'

const props = defineProps<{
	libraryId: string | null
}>()

const emit = defineEmits<{
	close: []
}>()

const isNew = computed(() => props.libraryId === 'new')
const libraries = librariesApi.useList()
const library = computed(() => libraries.data?.value?.find(l => l.id === props.libraryId))
const upsert = librariesApi.useUpsert()
const deleteLibrary = librariesApi.useDelete()

const form = useForm({
	schema: z.object({
		name: z.string().min(1),
		type: z.enum(['comics', 'books']),
	}),
	initialValues: {
		name: '',
		type: 'comics' as const,
	},
	onSubmit: async values => {
		await upsert.mutateAsync({
			id: isNew.value ? undefined : props.libraryId!,
			name: values.name,
			type: values.type,
			sources: library.value?.sources ?? [],
		})
		emit('close')
	},
})

watch(
	() => props.libraryId,
	() => {
		form.reset()
		if (props.libraryId === 'new') {
			form.setValues({ name: '', type: 'comics' })
		} else if (library.value) {
			form.setValues({ name: library.value.name, type: library.value.type })
		}
	},
	{ immediate: true }
)

watch(
	() => library.value,
	l => {
		if (l && props.libraryId !== 'new') {
			form.setValues({ name: l.name, type: l.type })
		}
	}
)

async function handleDelete() {
	if (!props.libraryId || isNew.value) return
	await deleteLibrary.mutateAsync(props.libraryId)
	emit('close')
}
</script>
