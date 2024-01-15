<template>
	<div class="acontainer flex flex-col gap-2">
		<div>
			<UInput v-model="searchTerm" placeholder="Search" />
		</div>
		<div>
			<div v-if="qQuery.isLoading.value && !results?.length">Loading...</div>
			<div v-else-if="qQuery.isError.value" class="text-red-500">
				{{ qQuery.error }}
			</div>
			<div v-else-if="!results?.length">No results</div>
			<div v-else class="flex flex-col">
				<UButton
					v-for="col in results"
					:to="'/' + slugify(col.name) + ':' + col.id"
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
	</div>
</template>

<script lang="ts" setup>
import type { InferAttributes } from 'sequelize'
import type { Collection } from '../server/models/collection'
import type { inferProcedureInput } from '@trpc/server'
import type { AppRouter } from '../server/trpc/routers'
import { useQuery } from '@tanstack/vue-query'
import { trpc } from '../plugins/trpc'
import slugify from 'slugify'
import { useUrlSearchParams } from '@vueuse/core'

const params = useUrlSearchParams('history')
const searchTerm = computed({
	get() {
		return typeof params.q === 'string' ? params.q : ''
	},
	set(value) {
		params.q = value
	}
})
const results = ref([]) as Ref<InferAttributes<Collection>[]>

const queryData = computed(() => {
	return <inferProcedureInput<AppRouter['items']['query']>>{
		title: searchTerm.value.trim()
	}
})
const qQuery = useQuery({
	queryKey: [
		'collection-query',
		computed(() => JSON.stringify(unref(queryData)))
	],
	async queryFn() {
		return trpc.collections.query.query(unref(queryData))
	}
})
watch(
	() => qQuery.data.value,
	value => {
		results.value = value ?? []
	},
	{ immediate: true }
)
</script>

<style></style>
