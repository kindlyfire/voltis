<template>
	<div class="acontainer">
		<div>
			<UInput v-model="searchTerm" placeholder="Search" />
		</div>
		<div>
			<div v-for="item in qQuery.data.value">
				{{ item.name }}
			</div>
		</div>
	</div>
</template>

<script lang="ts" setup>
import type { InferAttributes } from 'sequelize'
import { useCollectionQuery } from '../state/composables/queries'
import type { Collection } from '../server/models/collection'

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
