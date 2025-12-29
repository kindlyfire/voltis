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
				<!-- Form -->
				<template
					v-if="!scan.isPending.value && !scan.isSuccess.value && !scan.isError.value"
				>
					<VCheckbox
						class="force-scan-checkbox"
						v-model="forceScan"
						label="Force scan"
						:messages="[
							'Force scanning will re-scan all files, even if they have not changed since the last scan.',
						]"
					/>
					<div class="flex justify-end mt-6 gap-2">
						<VBtn variant="text" @click="$emit('update:modelValue', false)"
							>Cancel</VBtn
						>
						<VBtn color="primary" @click="startScan">Start Scan</VBtn>
					</div>
				</template>

				<!-- Scanning -->
				<template v-else-if="scan.isPending.value">
					<div class="text-center py-4">
						<VProgressCircular indeterminate class="mb-4" />
						<div>Scanning libraries...</div>
					</div>
				</template>

				<!-- Results -->
				<template v-else>
					<AQueryError :mutation="scan" />
					<div v-if="scan.isSuccess.value" class="space-y-2!">
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
					<div class="flex justify-end mt-4">
						<VBtn variant="text" @click="$emit('update:modelValue', false)">Close</VBtn>
					</div>
				</template>
			</VCardText>
		</VCard>
	</VDialog>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { librariesApi } from '@/utils/api/libraries'
import AQueryError from '@/components/AQueryError.vue'

const props = defineProps<{
	libraryIds: string[]
	modelValue: boolean
}>()

defineEmits<{
	'update:modelValue': [boolean]
}>()

const libraries = librariesApi.useList()
const scan = librariesApi.useScan()
const forceScan = ref(false)

const title = computed(() => {
	if (props.libraryIds.length === 0) {
		return 'Scan All Libraries'
	}
	if (props.libraryIds.length === 1) {
		return `Scan ${getLibraryName(props.libraryIds[0]!)}`
	}
	return `Scan ${props.libraryIds.length} Libraries`
})

function getLibraryName(id: string): string {
	return libraries.data?.value?.find(l => l.id === id)?.name ?? id
}

function startScan() {
	scan.mutate({
		ids: props.libraryIds.length > 0 ? props.libraryIds : undefined,
		force: forceScan.value,
	})
}

watch(
	() => props.modelValue,
	open => {
		if (open) {
			scan.reset()
			forceScan.value = false
		}
	}
)
</script>

<style lang="css" scoped>
:deep(.force-scan-checkbox .v-input__details) {
	margin-left: 40px;
	margin-top: -15px;
}

:deep(.force-scan-checkbox .v-messages__message) {
	line-height: 15px;
}
</style>
