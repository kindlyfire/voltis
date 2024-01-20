<template>
	<AMainWrapper>
		<template #side>
			<img
				:src="'/api/cover?width=640&collection-id=' + collection.id"
				alt=""
				class="rounded-lg shadow hidden md:block"
			/>
			<div class="md:hidden flex w-full gap-2">
				<img
					:src="'/api/cover?width=640&collection-id=' + collection.id"
					alt=""
					class="rounded-lg shadow max-w-[125px] w-full"
				/>
				<div class="flex flex-col gap-2">
					<PageTitle :title="collection.name" />

					<div>
						<UButton size="lg">
							<UIcon name="ph:book-open-bold" dynamic class="h-4 scale-[1.4]" />
							Start reading
						</UButton>
					</div>
				</div>
			</div>

			<!-- <UAlert
				v-if="collection.missing"
				title="The files for this collection were missing during the last scan and may be unavailable."
				color="red"
				variant="subtle"
			/> -->

			<div class="grid gap-2 grid-cols-2 md:gap-4 md:grid-cols-1">
				<div class="flex flex-col" v-if="metadata.pubStatus">
					<div class="flex items-center gap-1 text-sm text-muted">
						<UIcon name="ph:spinner-bold" dynamic class="scale-[1.2]" />
						Publication
					</div>
					<div :class="['-mt-1', metadata.pubYear == null && 'capitalize']">
						{{
							(metadata.pubYear ? metadata.pubYear + ', ' : '') +
							metadata.pubStatus
						}}
					</div>
				</div>
				<div class="flex flex-col" v-if="metadata.authors?.length">
					<div class="flex items-center gap-1 text-sm text-muted">
						<UIcon name="ph:user-bold" dynamic class="scale-[1.2]" />
						By
					</div>
					<div class="-mt-1">{{ metadata.authors.join(', ') }}</div>
				</div>
				<!-- <div class="flex flex-col">
					<div class="flex items-center gap-1 text-sm text-muted">
						<UIcon name="ph:link-bold" dynamic class="scale-[1.2]" />
						Links
					</div>
					<div class="-mt-1">
						<NuxtLink
							v-if="
								sourceMangadex &&
								(sourceMangadex.overrideRemoteId || sourceMangadex.remoteId)
							"
							:to="
								'https://mangadex.org/title/' +
								(sourceMangadex?.overrideRemoteId || sourceMangadex?.remoteId)
							"
							target="_blank"
							class="text-primary hover:underline"
						>
							Mangadex
						</NuxtLink>
					</div>
				</div> -->
				<div class="flex flex-col">
					<div class="flex items-center gap-1 text-sm text-muted">
						<UIcon name="ph:clock-bold" dynamic class="scale-[1.2]" />
						Added
					</div>
					<div class="-mt-1">{{ formatDate(collection.createdAt) }}</div>
				</div>
			</div>
		</template>

		<template #main>
			<PageTitle :title="collection.name" class="hidden md:block" />
			<div class="hidden md:block">
				<UButton size="lg">
					<UIcon name="ph:book-open-bold" dynamic class="h-4 scale-[1.4]" />
					Start reading
				</UButton>
			</div>
			<div>
				<Description :text="metadata.description || 'No description.'" />
			</div>
			<ChapterList :q-items="qItems" />
		</template>
	</AMainWrapper>
</template>

<script lang="ts" setup>
import { useQuery } from '@tanstack/vue-query'
import { trpc } from '../../../../plugins/trpc'
import { useItems } from '../../../../state/composables/queries'
import { formatDate } from '../../../../utils'
import Description from './Description.vue'
import ChapterList from './ChapterList.vue'

const route = useRoute()
const collectionId = computed(() =>
	typeof route.params.collectionId === 'string'
		? route.params.collectionId.split(':').at(-1) || ''
		: ''
)

const qCollection = useQuery({
	queryKey: ['collection', collectionId],
	async queryFn() {
		return await trpc.collections.get.query({ id: collectionId.value })
	},
	enabled: computed(() => !!collectionId.value)
})
await qCollection.suspense()
const collection = computed(() => qCollection.data.value!)
const metadata = computed(() => collection.value?.metadata.comic! ?? {})
const sourceMangadex = computed(() => {
	return collection.value?.metadata.comic
})

const qItems = useItems(
	computed(() =>
		collection.value ? { collectionId: collection.value.id } : null
	),
	{ enabled: computed(() => !!collection.value) }
)

useHead({
	title: computed(() => collection.value?.name ?? 'Loading...')
})
</script>
