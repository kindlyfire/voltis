<template>
	<VContainer>
		<div class="d-flex align-center mb-6">
			<h1 class="text-h4">My Lists</h1>
			<VSpacer />
			<VBtn color="primary" @click="openCreate">Create List</VBtn>
		</div>

		<VTable>
			<thead>
				<tr>
					<th>Name</th>
					<th>Visibility</th>
					<th>Entries</th>
					<th>Updated</th>
					<th></th>
				</tr>
			</thead>
			<tbody>
				<tr v-for="list in lists.data?.value" :key="list.id">
					<td>
						<RouterLink :to="`/${list.id}`" class="text-body-1 text-primary">
							{{ list.name }}
						</RouterLink>
					</td>
					<td class="text-capitalize">{{ list.visibility }}</td>
					<td>{{ list.entry_count ?? 0 }}</td>
					<td>{{ new Date(list.updated_at).toLocaleString() }}</td>
					<td>
						<VBtn
							icon="mdi-pencil"
							variant="text"
							size="small"
							@click="openEdit(list.id)"
						/>
					</td>
				</tr>
			</tbody>
		</VTable>

		<ListModal :list-id="selectedListId" v-model="modalOpen" />
	</VContainer>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useHead } from '@unhead/vue'
import { customListsApi } from '@/utils/api/custom-lists'
import ListModal from './ListModal.vue'

useHead({
	title: 'Lists',
})

const lists = customListsApi.useList('me')
const modalOpen = ref(false)
const selectedListId = ref<string | null>(null)

function openCreate() {
	selectedListId.value = 'new'
	modalOpen.value = true
}

function openEdit(id: string) {
	selectedListId.value = id
	modalOpen.value = true
}
</script>
