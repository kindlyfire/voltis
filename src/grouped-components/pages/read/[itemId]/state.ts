import slugify from 'slugify'
import type { InjectionKey } from 'vue'
import {
	useCollection,
	useItem,
	useItems,
	useReaderData
} from '../../../../state/composables/queries'
import type { FileMetadataCustomData } from '../../../../server/scanning/comic/metadata-file'

interface ReaderState {
	pageIndex: number
	mode: 'pages' | 'longstrip' | null
	pages: Array<ReturnType<typeof createPageState>>
}

export const readerStateKey = Symbol() as InjectionKey<
	ReturnType<typeof useComicReaderStore>
>

// Cache the reader modes per collection so that switching between chapters
// doesn't change the mode
const collectionReaderModes = reactive(new Map<string, ReaderState['mode']>())

const PREFETCH_COUNT = 3
export const useComicReaderStore = () => {
	const toast = useToast()
	const itemId = ref(null as string | null)

	const qItem = useItem(itemId)
	const item = computed(() => qItem.data.value)

	const qCollection = useCollection(computed(() => item.value?.collectionId))
	const collection = computed(() => qCollection.data.value)

	const qItems = useItems(
		computed(() =>
			collection.value ? { collectionId: collection.value.id } : null
		)
	)
	const items = computed(() => qItems.data.value)

	const qReaderData = useReaderData(itemId)
	const readerData = computed(() => qReaderData.data.value)

	const readerState = reactive({
		pageIndex: 0,
		mode: null,
		pages: []
	}) as ReaderState

	watch(
		() => [readerData.value, collection.value] as const,
		([rd, collection]) => {
			const cachedMode = collection
				? collectionReaderModes.get(collection.id)
				: null
			if (readerState.mode) return
			readerState.mode = cachedMode ?? rd?.suggestedMode ?? 'pages'
		}
	)
	watch(
		() => [readerState.mode, collection.value] as const,
		([mode, collection]) => {
			if (mode && collection) collectionReaderModes.set(collection.id, mode)
		}
	)
	watch(
		() => readerData.value,
		() => {
			if (!readerData.value) return
			readerState.pages = readerData.value.files.map(f =>
				createPageState(itemId.value!, f)
			)
		},
		{ immediate: true }
	)
	setupAutoPageFetch(readerState)

	const readerPages = usePagesToRender(readerState)

	function switchPage(offset: 1 | -1) {
		const newIndex = readerState.pageIndex + offset
		if (newIndex < 0 || newIndex >= readerState.pages.length) {
			switchChapter(offset)
			return
		}
		readerState.pageIndex = newIndex
	}
	function switchMode() {
		readerState.mode = readerState.mode === 'pages' ? 'longstrip' : 'pages'
	}
	function switchChapter(offset: 1 | -1) {
		const currentIndex =
			items.value?.findIndex(i => i.id === itemId.value!) ?? -1
		if (currentIndex === -1) {
			return
		}
		const nextIndex = currentIndex + offset * -1 // items list is sorted in reverse
		if (nextIndex < 0 || nextIndex >= items.value!.length) {
			if (collection.value) {
				navigateTo(
					'/' + slugify(collection.value.name) + ':' + collection.value.id
				)
				if (nextIndex < 0)
					toast.add({
						title: 'No more chapters',
						timeout: 2000
					})
			}
			return
		}
		const nextItem = items.value![nextIndex]
		navigateTo('/read/' + nextItem.id)
		toast.add({
			title: 'Reading ' + nextItem.name,
			timeout: 2000
		})
	}

	return {
		itemId,
		qItem,
		item,
		qCollection,
		collection,
		qReaderData,
		readerData,
		readerMode: computed(() => readerState.mode ?? 'pages'),
		readerState,
		readerPages,
		switchPage,
		switchMode,
		switchChapter,

		error: computed(() => qItem.error.value || qReaderData.error.value),
		loading: computed(
			() => qItem.isLoading.value || qReaderData.isLoading.value
		)
	}
}

function createPageState(
	itemId: string,
	f: FileMetadataCustomData['files'][0]
) {
	let fetchPromise: Promise<void> | null = null
	async function doFetch() {
		const res = await fetch(
			'/api/comic-page?item-id=' +
				encodeURIComponent(itemId) +
				'&file-name=' +
				encodeURIComponent(f.name)
		)
		if (!res.ok) {
			throw new Error('Failed to fetch page')
		}
		const blob = await res.blob()
		state.blobUrl = URL.createObjectURL(blob)
		state.error = null
		fetchPromise = null
	}

	const state = reactive({
		file: f,
		error: null as string | null,
		blobUrl: null as string | null,
		fetch() {
			if (fetchPromise || state.blobUrl) return
			fetchPromise = doFetch().catch(e => {
				console.error('Failed to fetch page', e)
				state.error = e.message ?? '' + e
			})
		}
	})

	return state
}

function setupAutoPageFetch(readerState: ReaderState) {
	watch(
		() => [readerState.pageIndex, readerState.pages.map(p => p.blobUrl)],
		() => {
			const pages = readerState.pages
			if (pages.length === 0) return
			const p = pages[readerState.pageIndex]
			p.fetch()

			if (!p.blobUrl) return
			if (readerState.pageIndex > 0) pages[readerState.pageIndex - 1].fetch()
			for (
				let i = readerState.pageIndex + 1;
				i < readerState.pageIndex + 1 + PREFETCH_COUNT && i < pages.length;
				i++
			) {
				pages[i].fetch()
			}
		}
	)
}

function usePagesToRender(readerState: ReaderState) {
	return computed(() => {
		const pages = readerState.pages
		if (pages.length === 0) return []

		if (readerState.mode === 'pages') {
			const p = pages[readerState.pageIndex]
			p.fetch()
			return [p]
		} else if (readerState.mode === 'longstrip') {
			return pages.slice(0, readerState.pageIndex + 2)
		} else {
			return []
		}
	})
}
