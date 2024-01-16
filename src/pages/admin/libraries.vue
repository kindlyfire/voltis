<template>
	<AMainWrapper>
		<template #side>
			<AdminPagesSidebar />
		</template>
		<template #main>
			<PageTitle title="Libraries" pagetitle="Libraries (Admin)" />

			<div class="flex items-center gap-2">
				<UButton size="lg" @click="addEditModalOpen = true">
					<UIcon
						name="ph:folder-notch-plus-bold"
						dynamic
						class="h-4 scale-[1.4]"
					/>
					Add library
				</UButton>
				<UButton
					size="lg"
					color="gray"
					@click="mScanLibraries.mutate(libraries?.map(l => l.id!) ?? [])"
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

			<div v-if="qLibraries.isPending.value">Loading...</div>
			<div v-else-if="!libraries?.length">
				No libraries set up yet. Add one to get started!
			</div>
			<div v-else class="grid lg:grid-cols-2 gap-4">
				<div v-for="lib in libraries" class="card rounded-md flex items-center">
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
										addEditModalLibraryId = lib.id!
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

	<AddLibraryModal
		:model-value="addEditModalOpen"
		@update:model-value="
			$event => {
				addEditModalOpen = $event
				addEditModalLibraryId = null
			}
		"
		:library-id="addEditModalLibraryId"
	/>
</template>

<script lang="ts" setup>
import { useMutation } from '@tanstack/vue-query'
import AddLibraryModal from '../../components/admin/AddLibraryModal.vue'
import AdminPagesSidebar from '../../components/admin/AdminPagesSidebar.vue'
import { useLibraries } from '../../state/composables/queries'
import { trpc } from '../../plugins/trpc'

const qLibraries = useLibraries({})
await qLibraries.suspense()
const libraries = qLibraries.data

const addEditModalOpen = ref(false)
const addEditModalLibraryId = ref(null) as Ref<string | null>

const mScanLibraries = useMutation({
	async mutationFn(ids: string[]) {
		if (ids.length === 0) return
		await trpc.scan.scanLibraries.mutate({
			libraryIds: ids
		})
		await qLibraries.refetch()
	}
})
</script>

<style></style>
