<template>
	<div class="acontainer !mb-4" v-if="currentChapterData">
		<PageTitle
			:pagetitle="
				currentChapterData.title + ' - ' + currentChapterData.collectionTitle
			"
		/>
		<div class="text-lg font-semibold">
			{{ currentChapterData.title }}
		</div>
		<div>
			<NuxtLink
				:to="
					'/' +
					slugify(currentChapterData.collectionTitle) +
					':' +
					currentChapterData.collectionId
				"
				class="text-primary hover:underline"
			>
				{{ currentChapterData.collectionTitle }}
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
import type { ChapterData, ReaderProvider } from './reader-core/types'
import { SwitchChapterDirection } from './reader-core/use-reader'

const route = useRoute()
const router = useRouter()
const itemId = computed(() => {
	return typeof route.params.itemId === 'string' ? route.params.itemId : null
})
const toast = useToast()
const readerRef = ref<InstanceType<typeof Reader>>()

const currentChapterData = ref(null as null | ChapterData)

const provider: ReaderProvider = {
	getChapterId() {
		if (!itemId.value) throw new Error('No chapter to load.')
		return itemId.value
	},

	async fetchChapterData(id) {
		const data = await trpc.items.getReaderData.query({ id })
		let startPage =
			typeof route.params.page === 'string' ? +route.params.page : 0
		startPage = Math.max(0, Math.min(startPage, data.pages.length - 1))
		return {
			id,
			title: data.chapterTitle,
			collectionId: data.collectionId,
			collectionTitle: data.collectionTitle,
			collectionLink:
				'/' + slugify(data.collectionTitle) + ':' + data.collectionId,
			pages: data.pages.map(p => ({
				...p,
				url:
					'/api/comic-page?ditem-id=' +
					encodeURIComponent(data.diskItemId) +
					'&file-name=' +
					encodeURIComponent(p.name)
			})),
			startPage,
			suggestedMode: data.suggestedMode
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

	beforeChapterChange(chapter) {
		toast.add({
			title: 'Reading ' + chapter.title,
			timeout: 2000
		})
	},

	onPageChange(page) {
		if (!currentChapterData.value) return
		if (route.fullPath.includes(currentChapterData.value.id))
			router.replace('/read/' + currentChapterData.value.id + '/' + page)
		else router.push('/read/' + currentChapterData.value.id + '/' + page)
	},

	afterChapterChange(chapter) {
		currentChapterData.value = chapter
	},

	onChapterChangeBeyondAvailable(direction) {
		if (direction === SwitchChapterDirection.Forward) {
			toast.add({
				title: 'No more chapters',
				timeout: 2000
			})
		}
		const chapter = readerRef.value!.reader.activeChapter.value
		if (!chapter) return
		router.push(
			'/' + slugify(chapter.collectionTitle) + ':' + chapter.collectionId
		)
	}
}
</script>

<style></style>
