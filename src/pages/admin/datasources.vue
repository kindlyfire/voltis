<template>
	<AMainWrapper>
		<template #side>
			<AdminPagesSidebar />
		</template>
		<template #main>
			<PageTitle title="Data Sources" pagetitle="Data Sources (Admin)" />

			<div class="flex items-center gap-2">
				<UButton size="lg" @click="addEditModalOpen = true">
					<UIcon
						name="ph:folder-notch-plus-bold"
						dynamic
						class="h-4 scale-[1.4]"
					/>
					Add data source
				</UButton>
				<UButton
					size="lg"
					color="gray"
					@click="mScanLibraries.mutate(dataSources?.map(l => l.id!) ?? [])"
					:loading="mScanLibraries.isPending.value"
				>
					<UIcon
						name="ph:arrows-clockwise-bold"
						dynamic
						class="h-4 scale-[1.4]"
					/>
					Scan all
				</UButton>
			</div>

			<div v-if="qDataSources.isPending.value">Loading...</div>
			<div v-else-if="!dataSources?.length">
				No data sources set up yet. Add one to get started!
			</div>
			<div v-else class="grid lg:grid-cols-2 gap-4">
				<div
					v-for="lib in dataSources"
					class="card rounded-md flex items-center"
				>
					<div>
						<div>{{ lib.name }}</div>
						<div class="text-muted">{{ lib.collectionCount }} collections</div>
					</div>
					<div class="ml-auto">
						<UButton
							color="gray"
							variant="ghost"
							@click="mScanLibraries.mutate([lib.id!])"
							:loading="mScanLibraries.isPending.value && mScanLibraries.variables.value?.includes(lib.id!)"
						>
							<UIcon
								name="ph:arrows-clockwise-bold"
								dynamic
								square
								class="scale-[1.4]"
							/>
						</UButton>
						<UButton
							color="gray"
							variant="ghost"
							@click="
									() => {
										addEditModalOpen = true
										addEditModalDataSourceId = lib.id!
									}
								"
						>
							<UIcon
								name="ph:pencil-simple-bold"
								dynamic
								square
								class="scale-[1.4]"
							/>
						</UButton>
					</div>
				</div>
			</div>
		</template>
	</AMainWrapper>

	<AddDataSourceModal
		:model-value="addEditModalOpen"
		@update:model-value="
			$event => {
				addEditModalOpen = $event
				addEditModalDataSourceId = null
			}
		"
		:data-source-id="addEditModalDataSourceId"
	/>
</template>

<script lang="ts" setup>
import { useMutation } from '@tanstack/vue-query'
import AddDataSourceModal from '../../components/admin/AddDataSourceModal.vue'
import AdminPagesSidebar from '../../components/admin/AdminPagesSidebar.vue'
import { useDataSources } from '../../state/composables/queries'
import { trpc } from '../../plugins/trpc'

const qDataSources = useDataSources({})
await qDataSources.suspense()
const dataSources = qDataSources.data

const addEditModalOpen = ref(false)
const addEditModalDataSourceId = ref(null) as Ref<string | null>

const mScanLibraries = useMutation({
	async mutationFn(ids: string[]) {
		if (ids.length === 0) return
		await trpc.scan.scanDataSources.mutate({
			dataSourceIds: ids
		})
		await qDataSources.refetch()
	}
})
</script>

<style></style>
