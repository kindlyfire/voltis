<template>
	<div class="acontainer !mb-4" v-if="activeChapter">
		<PageTitle
			:pagetitle="activeChapter.title + ' - ' + activeChapter.collection.title"
		/>
		<div class="text-lg font-semibold">
			{{ activeChapter.title }}
		</div>
		<div>
			<NuxtLink
				:to="
					routeBuilder['/c/[collectionId]/[name]'](
						activeChapter.collection.id,
						activeChapter.collection.title
					)
				"
				class="text-primary hover:underline"
			>
				{{ activeChapter.collection.title }}
			</NuxtLink>
		</div>
	</div>
	<Reader :provider="provider" ref="readerRef" />
	<div class="-mt-4"></div>
</template>

<script lang="ts" setup>
import slugify from 'slugify'
import { trpc } from '../../../../plugins/trpc'
import Reader from './reader-core/Reader.vue'
import {
	SwitchChapterDirection,
	type ReaderProvider
} from './reader-core/types'
import { useDebounceFn } from '@vueuse/core'

const route = useRoute()
const router = useRouter()
const itemId = computed(() => {
	return typeof route.params.itemId === 'string' ? route.params.itemId : null
})
const toast = useToast()
const readerRef = ref<InstanceType<typeof Reader>>()
const loadingIndicator = useLoadingIndicator()

const activeChapter = computed(() => {
	return readerRef.value?.reader.activeChapter.value
})

const provider: ReaderProvider<{
	page: number
	updateProgress: () => Promise<void>
	updateProgressDebounced: () => Promise<void>
}> = {
	getChapterId() {
		if (!itemId.value) throw new Error('No chapter to load.')
		return itemId.value
	},

	async fetchChapterData(id) {
		const data = await trpc.items.getReaderData.query({ id })
		let startPage =
			typeof route.params.page === 'string' ? +route.params.page : 0
		startPage = Math.max(0, Math.min(startPage, data.pages.length - 1))

		let completed = data.userProgress?.completed ?? false

		const customData = {
			page: startPage,
			async updateProgress() {
				if (completed) return
				completed = customData.page === data.pages.length - 1
				await trpc.items.updateUserData
					.mutate({
						itemId: id,
						completed,
						progress:
							customData.page === data.pages.length - 1
								? null
								: {
										page: customData.page
								  }
					})
					.catch(e => {
						console.error(e)
						toast.add({
							title: 'Failed to update progress',
							timeout: 2000
						})
					})
			},
			async updateProgressDebounced() {}
		}
		customData.updateProgressDebounced = useDebounceFn(
			customData.updateProgress,
			3000,
			{ maxWait: 5000 }
		)

		return {
			id,
			title: data.chapterTitle,
			collection: {
				id: data.collectionId,
				title: data.collectionTitle,
				link: routeBuilder['/c/[collectionId]/[name]'](
					data.collectionId,
					slugify(data.collectionTitle)
				)
			},
			pages: data.pages.map(p => ({
				...p,
				url:
					'/api/comic-page?ditem-id=' +
					encodeURIComponent(data.diskItemId) +
					'&file-name=' +
					encodeURIComponent(p.name)
			})),
			page: startPage,
			mode: data.suggestedMode,
			custom: customData
		}
	},

	async getChapterList() {
		if (!itemId.value) throw new Error('No chapter to load.')
		const items = await trpc.items.query.query({
			inSameCollectionAs: itemId.value
		})
		return items.map(i => ({
			id: i.id,
			title: i.name
		}))
	},

	beforeChapterChange(ev) {
		toast.add({
			title: 'Reading ' + ev.chapter.title,
			timeout: 2000
		})
	},

	onPageChange(ev) {
		const url = routeBuilder['/read/[itemId]/[page]'](ev.chapter.id, ev.value)
		if (route.fullPath.includes(ev.chapter.id)) router.replace(url)
		else router.push(url)
		ev.custom.page = ev.value
		ev.custom.updateProgressDebounced()
	},

	afterChapterChange() {},

	async onChapterChangeBeyondAvailable(ev) {
		if (ev.custom) {
			loadingIndicator.start()
			await ev.custom.updateProgress()
			loadingIndicator.finish()
		}
		if (ev.direction === SwitchChapterDirection.Forward) {
			toast.add({
				title: 'No more chapters',
				timeout: 2000
			})
		}
		if (!ev.chapter) return
		router.push(
			routeBuilder['/c/[collectionId]/[name]'](
				ev.chapter.collection.id,
				slugify(ev.chapter.collection.title)
			)
		)
	}
}
</script>

<style></style>
