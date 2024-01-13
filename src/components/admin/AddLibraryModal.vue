<template>
	<UModal
		:model-value="props.modelValue"
		@update:model-value="emit('update:modelValue', $event)"
		:ui="{
			height: 'min-h-[20rem]',
			width: 'w-[40rem] sm:max-w-[40rem]'
		}"
		class=""
	>
		<div class="flex items-center p-4">
			<div class="font-bold">{{ libraryId ? 'Edit' : 'Add' }} library</div>
			<div class="ml-auto">
				<UButton
					@click="emit('update:modelValue', false)"
					color="gray"
					variant="ghost"
				>
					<UIcon name="ph:x" dynamic class="h-5 scale-[1.4]" />
				</UButton>
			</div>
		</div>
		<hr />
		<div class="p-4 grow">
			<UForm
				ref="formRef"
				class="flex flex-col gap-4"
				:schema="schema"
				:state="state"
				@submit="mCreate.mutate()"
			>
				<UFormGroup label="Name" name="name" size="lg">
					<UInput v-model="state.name" />
				</UFormGroup>

				<UFormGroup label="Type" name="matcher" size="lg">
					<USelect
						v-model="state.matcher"
						:options="[{ name: 'Comics', value: 'comic' }]"
						option-attribute="name"
						value-attribute="value"
					/>
				</UFormGroup>

				<UFormGroup label="Paths" name="paths" size="lg">
					<div class="flex flex-col gap-1">
						<div class="py-2" v-if="state.paths.length > 0">
							<div v-for="p in state.paths" class="flex items-center gap-2">
								<UButton
									color="gray"
									size="xs"
									square
									@click="state.paths.splice(state.paths.indexOf(p), 1)"
								>
									<UIcon name="ph:x" dynamic class="scale-[1.4]" />
								</UButton>
								<div
									:title="p"
									class="overflow-hidden whitespace-nowrap text-ellipsis grow"
								>
									{{ p }}
								</div>
							</div>
						</div>

						<div class="flex gap-2">
							<UInput
								v-model="pathInputValue"
								class="grow"
								placeholder="Add a path"
								@keydown.enter.prevent="addPath"
							/>
							<UButton class="px-3" @click.prevent="addPath">
								<UIcon name="ph:plus-bold" dynamic class="h-4 scale-[1.4]" />
							</UButton>
						</div>
					</div>
				</UFormGroup>

				<div v-if="errorMessage" class="text-red-500">
					{{ errorMessage }}
				</div>

				<div class="flex items-center gap-2">
					<UButton type="submit" :loading="mCreate.isPending.value">
						{{ libraryId ? 'Save' : 'Create' }}
					</UButton>
					<UButton
						v-if="libraryId"
						@click.prevent.stop="mDelete.mutate()"
						:loading="mDelete.isPending.value"
						color="red"
					>
						Delete
					</UButton>
				</div>
			</UForm>
		</div>
	</UModal>
</template>

<script lang="ts" setup>
import { z } from 'zod'
import type { UForm } from '../../../.nuxt/components'
import { useMutation } from '@tanstack/vue-query'
import { trpc } from '../../plugins/trpc'
import { useLibraries } from '../../state/composables/queries'

const props = defineProps<{
	modelValue: boolean
	libraryId?: string | null
}>()
const emit = defineEmits<{
	'update:modelValue': [open: boolean]
}>()

const qLibraries = useLibraries()
const libraryId = ref(null) as Ref<string | null>

const schema = z.object({
	name: z.string().min(1, 'Must be at least 1 character'),
	matcher: z.enum(['comic']),
	paths: z.array(z.string()).min(1, 'Must have at least 1 path')
})
const state = reactive({
	name: '',
	matcher: 'comic',
	paths: []
}) as z.output<typeof schema>

const pathInputValue = ref('')
function addPath() {
	const v = pathInputValue.value.trim()
	if (v && !state.paths.includes(v)) {
		state.paths.push(v)
		pathInputValue.value = ''
	}
}

// Reset step when modal is opened
watch(
	() => props.modelValue,
	() => {
		if (props.modelValue) {
			state.name = ''
			state.matcher = 'comic'
			state.paths = []

			libraryId.value = props.libraryId ?? null
			const lib = qLibraries.data.value?.find(l => l.id === libraryId.value)
			if (lib) {
				state.name = lib.name!
				state.matcher = lib.matcher as any
				state.paths = lib.paths!
			}
		}
	}
)

const mCreate = useMutation({
	async mutationFn() {
		if (!props.libraryId) {
			await trpc.libraries.create.mutate({
				name: state.name,
				matcher: state.matcher,
				paths: state.paths
			})
		} else {
			await trpc.libraries.update.mutate({
				id: props.libraryId!,
				name: state.name,
				matcher: state.matcher,
				paths: state.paths
			})
		}
		await qLibraries.refetch()
		emit('update:modelValue', false)
	}
})
const errorMessage = computed(() => {
	const e = mCreate.error.value || mDelete.error.value
	if (!e) return
	if (e.name === 'TRPCError') {
		return e.message
	}
	return `${e.name}: ${e.message}`
})

const mDelete = useMutation({
	async mutationFn() {
		if (!props.libraryId) return
		await trpc.libraries.delete.mutate({ id: props.libraryId })
		await qLibraries.refetch()
		emit('update:modelValue', false)
	}
})
</script>

<style></style>
