<template>
	<div class="acontainer">
		<div class="flex flex-row gap-4">
			<AdminPagesSidebar />
			<div class="flex flex-col gap-4 grow">
				<div class="text-5xl font-bold">Libraries</div>

				<div class="flex items-center gap-2">
					<UButton size="lg" @click="addEditModalOpen = true">
						<UIcon
							name="ph:folder-notch-plus-bold"
							dynamic
							class="h-4 scale-[1.4]"
						/>
						Add library
					</UButton>
					<UButton size="lg" color="gray">
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
				<div v-else class="grid grid-cols-2">
					<div
						v-for="lib in libraries"
						class="card rounded-md flex items-center"
					>
						{{ lib.name }}
						<div class="ml-auto">
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
			</div>
		</div>

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
	</div>
</template>

<script lang="ts" setup>
import AddLibraryModal from '../../components/admin/AddLibraryModal.vue'
import AdminPagesSidebar from '../../components/admin/AdminPagesSidebar.vue'
import { useLibraries } from '../../state/composables/queries'

const qLibraries = useLibraries()
const libraries = qLibraries.data

const addEditModalOpen = ref(false)
const addEditModalLibraryId = ref(null) as Ref<string | null>
</script>

<style></style>
