<template>
	<VDialog
		:model-value="modelValue"
		@update:model-value="$emit('update:modelValue', $event)"
		max-width="500"
		persistent
	>
		<VCard>
			<VCardTitle>{{ title }}</VCardTitle>
			<VCardText>
				<div v-if="scan.isPending.value" class="text-center py-4">
					<VProgressCircular indeterminate class="mb-4" />
					<div>Scanning libraries...</div>
				</div>
				<div v-else-if="scan.isError.value">
					<VAlert type="error" class="mb-4">
						{{ scan.error.value?.message || 'An error occurred during scanning' }}
					</VAlert>
				</div>
				<div v-else-if="scan.data.value">
					<div class="space-y-2!">
						<div
							v-for="result in scan.data.value"
							:key="result.library_id"
							class="border rounded pa-3"
						>
							<div class="font-medium">{{ getLibraryName(result.library_id) }}</div>
							<div class="text-sm text-medium-emphasis">
								Added: {{ result.added }} | Updated: {{ result.updated }} | Removed:
								{{ result.removed }} | Unchanged: {{ result.unchanged }}
							</div>
						</div>
					</div>
				</div>
				<div class="flex justify-end mt-4">
					<VBtn
						variant="text"
						@click="$emit('update:modelValue', false)"
						:disabled="scan.isPending.value"
					>
						Close
					</VBtn>
				</div>
			</VCardText>
		</VCard>
	</VDialog>
</template>

<script setup lang="ts">
import { watch, computed } from 'vue'
import { librariesApi } from '@/utils/api/libraries'

const props = defineProps<{
	libraryIds: string[]
	modelValue: boolean
}>()

defineEmits<{
	'update:modelValue': [boolean]
}>()

const libraries = librariesApi.useList()
const scan = librariesApi.useScan()

const title = computed(() => {
	if (props.libraryIds.length === 0) {
		return 'Scan All Libraries'
	}
	if (props.libraryIds.length === 1) {
		return `Scan ${getLibraryName(props.libraryIds[0])}`
	}
	return `Scan ${props.libraryIds.length} Libraries`
})

function getLibraryName(id: string): string {
	return libraries.data?.value?.find(l => l.id === id)?.name ?? id
}

watch(
	() => props.modelValue,
	open => {
		if (open) {
			scan.reset()
			scan.mutate(props.libraryIds.length > 0 ? props.libraryIds : undefined)
		}
	}
)
</script>
