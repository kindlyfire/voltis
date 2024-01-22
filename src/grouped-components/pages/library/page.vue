<template>
	<div class="acontainer flex flex-col gap-4">
		<PageTitle title="Library" />
		<div class="flex items-start -mb-2 overflow-auto">
			<div class="shrink-0">
				<UTabs :items="libraryTypes" v-model="selectedIndex"></UTabs>
			</div>
		</div>

		<div v-if="collections.length === 0">No collections to show.</div>
		<div class="grid grid-cols-1 gap-2 md:grid-cols-2">
			<div v-for="col in collections" class="card flex overflow-hidden gap-2">
				<NuxtLink
					class="w-[80px] xs:w-[130px] md:w-[80px] lg:w-[130px] shrink-0"
					:to="
						routeBuilder['/c/[collectionId]/[name]'](
							col.collectionId,
							slugify(col.Collection.name)
						)
					"
				>
					<img
						class="cover rounded shadow"
						:src="'/api/cover?width=320&collection-id=' + col.collectionId"
						alt=""
					/>
				</NuxtLink>
				<div class="flex flex-col gap-1 grow justify-stretch">
					<NuxtLink
						class="font-semibold xs:text-lg block p-2 -m-2"
						:to="
							routeBuilder['/c/[collectionId]/[name]'](
								col.collectionId,
								slugify(col.Collection.name)
							)
						"
					>
						{{ col.Collection.name }}
					</NuxtLink>
					<div>
						<Markdown
							:text="
								col.Collection.metadata.comic?.description ?? 'No description.'
							"
						/>
					</div>
				</div>
			</div>
		</div>
	</div>
</template>

<script lang="ts" setup>
import { useQuery } from '@tanstack/vue-query'
import { trpc } from '../../../plugins/trpc'
import slugify from 'slugify'
import { libraryTypes } from '../../../constants'

const route = useRoute()
const router = useRouter()

const tabs = libraryTypes.map(x => x.type)

const tab = computed(() => {
	const v = route.query.tab
	return tabs.includes(v as any) ? (v as string) : 'reading'
})

const selectedIndex = computed({
	get() {
		return tabs.indexOf(tab.value)
	},
	set(index) {
		router.push({
			query: {
				tab: index === 0 ? undefined : tabs[index]
			}
		})
	}
})

const qCollections = useQuery({
	queryKey: ['library', tab],
	async queryFn() {
		const items = await trpc.customLists.getUserLibraryItems.query({
			type: tab.value as any
		})
		return items
	}
})
const collections = computed(() => qCollections.data.value ?? [])
</script>

<style></style>
