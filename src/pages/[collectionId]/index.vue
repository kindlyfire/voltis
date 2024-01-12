<template>
	<div class="acontainer flex flex-col gap-4">
		<div class="flex flex-row gap-4" v-if="collection">
			<div class="w-[300px] shrink-0 flex flex-col gap-2">
				<img
					:src="'/api/cover?collection-id=' + collection.id"
					alt=""
					class="rounded shadow"
				/>
				<div class="flex flex-col" v-if="collection.metadata.pubStatus">
					<div class="flex items-center gap-1 text-sm text-muted">
						<UIcon name="ph:spinner-bold" dynamic class="scale-[1.2]" />
						Publication
					</div>
					<div
						:class="[
							'-mt-1',
							collection.metadata.pubYear === null && 'capitalize'
						]"
					>
						{{
							(collection.metadata.pubYear
								? collection.metadata.pubYear + ', '
								: '') + collection.metadata.pubStatus
						}}
					</div>
				</div>
				<div class="flex flex-col" v-if="collection.metadata.authors?.length">
					<div class="flex items-center gap-1 text-sm text-muted">
						<UIcon name="ph:user-bold" dynamic class="scale-[1.2]" />
						By
					</div>
					<div class="-mt-1">{{ collection.metadata.authors.join(', ') }}</div>
				</div>
				<div class="flex flex-col" v-if="collection.metadata.authors?.length">
					<div class="flex items-center gap-1 text-sm text-muted">
						<UIcon name="ph:link-bold" dynamic class="scale-[1.2]" />
						Links
					</div>
					<div class="-mt-1">
						<NuxtLink
							v-if="collection.metadata?.mangadexId"
							:to="
								'https://mangadex.org/title/' + collection.metadata.mangadexId
							"
							target="_blank"
							class="text-primary hover:underline"
						>
							Mangadex
						</NuxtLink>
					</div>
				</div>
				<div class="flex flex-col">
					<div class="flex items-center gap-1 text-sm text-muted">
						<UIcon name="ph:clock-bold" dynamic class="scale-[1.2]" />
						Added
					</div>
					<div class="-mt-1">{{ formatDate(new Date()) }}</div>
				</div>
			</div>
			<div class="flex flex-col gap-4 grow">
				<div class="text-5xl font-bold">{{ collection.name }}</div>
				<div>
					<UButton size="lg">
						<UIcon name="ph:book-open-bold" dynamic class="h-4 scale-[1.4]" />
						Start reading
					</UButton>
				</div>
				<div>
					{{ collection.metadata?.description || 'No description.' }}
				</div>
				<div class="flex flex-col gap-1">
					<NuxtLink
						v-for="i in pageItems"
						:to="'/read/' + i.id"
						class="card w-full border-l-4 border-l-[rgb(var(--color-primary-DEFAULT)/0.75)]"
					>
						<div class="flex items-center gap-2">
							<div>
								<button class="flex items-center text-muted">
									<UIcon
										name="ph:check-square-offset-bold"
										dynamic
										class="scale-[1.2]"
									/>
								</button>
							</div>
							<div
								class="overflow-hidden whitespace-nowrap text-ellipsis font-semibold"
							>
								{{ i.name }}
							</div>
						</div>
					</NuxtLink>
				</div>
				<div class="flex items-center justify-center">
					<UPagination
						:page-count="pageSize"
						:total="items?.length ?? 0"
						v-model="page"
						show-last
						show-first
						size="lg"
					/>
				</div>
			</div>
		</div>
	</div>
</template>

<script lang="ts" setup>
import { trpc } from '../../plugins/trpc'
import { useQuery } from '@tanstack/vue-query'
import { formatDate } from '../../utils'

const route = useRoute()
const collectionId = computed(() =>
	typeof route.params.collectionId === 'string'
		? route.params.collectionId.slice(-13)
		: ''
)
const page = ref(1)
const pageSize = ref(50)
const pageItems = computed(() => {
	const start = (page.value - 1) * pageSize.value
	const end = start + pageSize.value
	return items.value?.slice(start, end) ?? []
})

const qCollection = useQuery({
	queryKey: ['collection', collectionId],
	async queryFn() {
		return await trpc.collections.get.query({ id: collectionId.value })
	},
	enabled: computed(() => !!collectionId.value)
})
const collection = qCollection.data

const qItems = useQuery({
	queryKey: ['items', collectionId],
	async queryFn() {
		return await trpc.items.list.query({ collectionId: collectionId.value })
	},
	enabled: computed(() => !!collectionId.value)
})
const items = qItems.data

useHead({
	title: computed(() => collection.value?.name ?? 'Loading...')
})
</script>
