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
		<div class="p-4">
			<div class="flex items-center gap-2">
				<div class="font-bold grow">
					<UInput v-model="searchTerm" placeholder="Search" autofocus>
						<template #trailing>
							<UIcon
								v-if="qQuery.isLoading.value"
								name="i-heroicons-arrow-path-20-solid"
								class="text-gray-400 dark:text-gray-500 h-5 w-5 animate-spin"
							/>
						</template>
					</UInput>
				</div>
				<div class="flex items-center">
					<UButton
						color="gray"
						variant="ghost"
						:to="'/search?q=' + encodeURIComponent(searchTerm)"
						@click="emit('update:modelValue', false)"
					>
						<UIcon
							name="ph:arrow-square-out-bold"
							dynamic
							class="scale-[1.2]"
						/>
						Advanced search
					</UButton>
					<UButton
						@click="emit('update:modelValue', false)"
						color="gray"
						variant="ghost"
					>
						<UIcon name="ph:x" dynamic class="h-5 scale-[1.4]" />
					</UButton>
				</div>
			</div>
		</div>
		<hr />
		<div class="p-4">
			<div v-if="qQuery.isLoading.value && !results?.length">Loading...</div>
			<div v-else-if="qQuery.isError.value" class="text-red-500">
				{{ qQuery.error }}
			</div>
			<div v-else-if="!results?.length">No results</div>
			<div v-else class="flex flex-col">
				<UButton
					v-for="col in results"
					:to="'/' + slugify(col.name) + ':' + col.id"
					@click="emit('update:modelValue', false)"
					color="gray"
					variant="ghost"
					square
				>
					<div class="flex flex-row gap-2">
						<div>
							<img
								class="cover h-16 rounded overflow-hidden"
								:src="'/api/cover?collection-id=' + col.id"
							/>
						</div>
						<div>
							<div class="text-base font-semibold">
								{{ col.name }}
							</div>
						</div>
					</div>
				</UButton>
			</div>
		</div>
	</UModal>
</template>

<script lang="ts" setup>
import slugify from 'slugify'
import { useCollectionQuery } from '../state/composables/queries'
import type { InferAttributes } from 'sequelize'
import type { Collection } from '../server/models/collection'

const props = defineProps<{
	modelValue: boolean
}>()
const emit = defineEmits<{
	'update:modelValue': [open: boolean]
}>()

const searchTerm = ref('')
const results = ref([]) as Ref<InferAttributes<Collection>[]>

const qQuery = useCollectionQuery(
	computed(() => {
		return {
			title: searchTerm.value
		}
	})
)
watch(
	() => qQuery.data.value,
	value => {
		results.value = value ?? []
	}
)
</script>

<style></style>
