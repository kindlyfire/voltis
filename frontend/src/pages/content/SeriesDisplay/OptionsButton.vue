<template>
	<VMenu :offset="6">
		<template #activator="{ props: menuProps }">
			<VBtn v-bind="menuProps" icon="mdi-dots-vertical" />
		</template>
		<VList>
			<VListItem
				prepend-icon="mdi-refresh"
				title="Reset progress"
				@click="mResetProgress.mutate()"
			/>
		</VList>
	</VMenu>
</template>

<script setup lang="ts">
import { contentApi } from '@/utils/api/content'
import { useMutation, useQueryClient } from '@tanstack/vue-query'

const props = defineProps<{
	contentId: string
}>()

const queryClient = useQueryClient()

const mResetProgress = useMutation({
	mutationFn: async () => {
		if (confirm('Are you sure you want to reset your progress for this series?')) {
			await contentApi.resetSeriesProgress(props.contentId)
			queryClient.invalidateQueries()
		}
	},
})
</script>
