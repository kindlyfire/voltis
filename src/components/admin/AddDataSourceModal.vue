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
		<div class="flex items-center padding-modal">
			<div class="font-bold">
				{{ dataSourceId ? 'Edit' : 'Add' }} data source
			</div>
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
		<div class="padding-modal grow">
			<UForm
				ref="formRef"
				class="flex flex-col gap-4"
				:schema="schema"
				:state="state"
				@submit="mSave.mutate()"
			>
				<UFormGroup label="Name" name="name" size="lg">
					<UInput v-model="state.name" />
				</UFormGroup>

				<UFormGroup label="Type" name="matcher" size="lg">
					<USelect
						v-model="state.type"
						:options="[{ name: 'Comics', value: 'comic' }]"
						option-attribute="name"
						value-attribute="value"
					/>
				</UFormGroup>

				<UFormGroup label="Paths" name="paths" size="lg">
					<div class="flex flex-col gap-2">
						<div class="flex flex-col gap-1" v-if="state.paths.length > 0">
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
					<UButton type="submit" :loading="mSave.isPending.value">
						{{ dataSourceId ? 'Save' : 'Create' }}
					</UButton>
					<UButton
						v-if="dataSourceId"
						@click.prevent.stop="mDelete.mutate()"
						:loading="mDelete.isPending.value"
						color="red"
						variant="soft"
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
import { useMutation } from '@tanstack/vue-query'
import { trpc } from '../../plugins/trpc'
import { useDataSources } from '../../state/composables/queries'

const props = defineProps<{
	modelValue: boolean
	dataSourceId?: string | null
}>()
const emit = defineEmits<{
	'update:modelValue': [open: boolean]
}>()

// InstanceType<typeof UForm> does not result in the correct types. I don't know
// what would.
const formRef = ref(null) as Ref<{ clear(path: string): void } | null>

const qLibraries = useDataSources({})
const dataSourceId = ref(null) as Ref<string | null>

const schema = z.object({
	name: z.string().min(1, 'Must be at least 1 character'),
	type: z.enum(['comic']),
	paths: z.array(z.string()).min(1, 'Must have at least 1 path')
})
const state = reactive({
	name: '',
	type: 'comic',
	paths: []
}) as z.output<typeof schema>

const pathInputValue = ref('')
function addPath() {
	const v = pathInputValue.value.trim()
	if (v && !state.paths.includes(v)) {
		state.paths.push(v)
		pathInputValue.value = ''
		formRef.value?.clear('paths')
	}
}

// Reset step when modal is opened
watch(
	() => props.modelValue,
	() => {
		if (props.modelValue) {
			state.name = ''
			state.type = 'comic'
			state.paths = []

			dataSourceId.value = props.dataSourceId ?? null
			const lib = qLibraries.data.value?.find(l => l.id === dataSourceId.value)
			if (lib) {
				state.name = lib.name!
				state.type = lib.type as any
				state.paths = [...lib.paths!]
			}
		}
	}
)

const mSave = useMutation({
	async mutationFn() {
		if (!props.dataSourceId) {
			await trpc.libraries.create.mutate({
				name: state.name,
				matcher: state.type,
				paths: state.paths
			})
		} else {
			await trpc.libraries.update.mutate({
				id: props.dataSourceId!,
				name: state.name,
				matcher: state.type,
				paths: state.paths
			})
		}
		await qLibraries.refetch()
		emit('update:modelValue', false)
	}
})
const errorMessage = computed(() => {
	const e = mSave.error.value || mDelete.error.value
	if (!e) return
	if (e.name === 'TRPCError') {
		return e.message
	}
	return `${e.name}: ${e.message}`
})

const mDelete = useMutation({
	async mutationFn() {
		if (!props.dataSourceId) return
		await trpc.libraries.delete.mutate({ id: props.dataSourceId })
		await qLibraries.refetch()
		emit('update:modelValue', false)
	}
})
</script>

<style></style>
