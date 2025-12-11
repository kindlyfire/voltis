<template>
	<VContainer>
		<div class="d-flex align-center mb-6">
			<h1 class="text-h4">Libraries</h1>
			<VSpacer />
			<VBtn color="primary" @click="selectedLibraryId = 'new'">Create Library</VBtn>
		</div>

		<VTable>
			<thead>
				<tr>
					<th>Name</th>
					<th>Type</th>
					<th>Last Scanned</th>
					<th></th>
				</tr>
			</thead>
			<tbody>
				<tr v-for="library in libraries.data?.value" :key="library.id">
					<td>{{ library.name }}</td>
					<td>{{ library.type }}</td>
					<td>
						{{
							library.scanned_at
								? new Date(library.scanned_at).toLocaleString()
								: 'Never'
						}}
					</td>
					<td>
						<VBtn
							icon="mdi-pencil"
							variant="text"
							size="small"
							@click="selectedLibraryId = library.id"
						/>
					</td>
				</tr>
			</tbody>
		</VTable>

		<LibraryModal :library-id="selectedLibraryId" @close="selectedLibraryId = null" />
	</VContainer>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { librariesApi } from '@/utils/api/libraries'
import LibraryModal from './LibraryModal.vue'

const libraries = librariesApi.useList()
const selectedLibraryId = ref<string | null>(null)
</script>