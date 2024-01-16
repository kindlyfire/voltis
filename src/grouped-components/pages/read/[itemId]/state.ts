import slugify from 'slugify'
import {
	useCollection,
	useItem,
	useItems,
	useReaderData
} from '../../../../state/composables/queries'
import type { FileMetadataCustomData } from '../../../../server/scanning/comic/metadata-file'

interface ReaderState {
	itemId: string | null
	pageIndex: number
	mode: 'pages' | 'longstrip' | null
	pages: Array<ReturnType<typeof createPageState>>
}

const defaultReaderState: ReaderState = {
	itemId: null,
	pageIndex: 0,
	mode: null,
	pages: []
}

const PREFETCH_COUNT = 10
export const useComicReaderStore = defineStore('comic-reader', () => {
	const toast = useToast()
	const route = useRoute()

	const itemId = ref(null as string | null)
	const menuOpen = ref(false)

	// Cache the reader modes per collection so that switching between chapters
	// doesn't change the mode
	const collectionReaderModes = reactive(new Map<string, ReaderState['mode']>())

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

	let cachedReaderState = reactive({ ...defaultReaderState })
	const readerState = computed(() => {
		if (cachedReaderState.itemId !== itemId.value) {
			cachedReaderState = reactive({ ...defaultReaderState })
			cachedReaderState.itemId = itemId.value
		}
		return cachedReaderState
	})

	watch(
		() => [readerData.value, collection.value] as const,
		([rd, collection]) => {
			if (readerState.value.mode || !readerData.value) return
			const cachedMode = collection
				? collectionReaderModes.get(collection.id)
				: null
			readerState.value.mode =
				cachedMode ?? rd?.suggestedMode ?? readerState.value.mode
			if ('end' in route.query) {
				readerState.value.pageIndex = readerData.value.files.length - 1
				navigateTo(route.path, { replace: true })
			}
		}
	)
	watch(
		() => [readerState.value.mode, collection.value] as const,
		([mode, collection]) => {
			if (mode && collection) collectionReaderModes.set(collection.id, mode)
		}
	)
	watch(
		() => readerData.value,
		() => {
			if (!readerData.value) return
			readerState.value.pages = readerData.value.files.map(f =>
				createPageState(itemId.value!, f)
			)
		},
		{ immediate: true }
	)
	setupAutoPageFetch(readerState)

	const readerPages = usePagesToRender(readerState)

	function switchPage(offset: 1 | -1) {
		const newIndex = readerState.value.pageIndex + offset
		if (newIndex < 0 || newIndex >= readerState.value.pages.length) {
			switchChapter(offset)
			return
		}
		readerState.value.pageIndex = newIndex
	}
	function switchMode() {
		readerState.value.mode =
			readerState.value.mode === 'pages' ? 'longstrip' : 'pages'
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
				menuOpen.value = false
				if (nextIndex < 0)
					toast.add({
						title: 'No more chapters',
						timeout: 2000
					})
			}
			return
		}
		const nextItem = items.value![nextIndex]
		navigateTo('/read/' + nextItem.id + (offset === -1 ? '?end' : ''))
		toast.add({
			title: 'Reading ' + nextItem.name,
			timeout: 2000
		})
	}

	return {
		menuOpen,
		itemId,
		qItem,
		item,
		items,
		qCollection,
		collection,
		qReaderData,
		readerData,
		readerMode: computed(() => readerState.value.mode ?? 'pages'),
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
})

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
				fetchPromise = null
			})
		}
	})

	return state
}

function setupAutoPageFetch(readerState: Ref<ReaderState>) {
	watch(
		() => [
			readerState.value.pageIndex,
			readerState.value.pages.map(p => p.blobUrl)
		],
		() => {
			const pages = readerState.value.pages
			if (pages.length === 0) return
			const p = pages[readerState.value.pageIndex]
			p.fetch()

			if (!p.blobUrl) return
			for (
				let i = 1, prefetched = 0;
				i <= PREFETCH_COUNT && prefetched < PREFETCH_COUNT;
				i++
			) {
				if (readerState.value.pageIndex + i < pages.length) {
					pages[readerState.value.pageIndex + i].fetch()
					prefetched++
				}
				if (readerState.value.pageIndex - i >= 0) {
					pages[readerState.value.pageIndex - i].fetch()
					prefetched++
				}
			}
		}
	)
}

function usePagesToRender(readerState: Ref<ReaderState>): ComputedRef<{
	all: ReaderState['pages'][0][]
	hidden: ReaderState['pages'][0][]
}> {
	return computed(() => {
		const pages = readerState.value.pages
		if (pages.length === 0)
			return {
				all: [],
				hidden: []
			}

		if (readerState.value.mode === 'pages') {
			const p = pages[readerState.value.pageIndex]
			p.fetch()

			const all = pages.slice(
				Math.max(0, readerState.value.pageIndex - 2),
				Math.min(pages.length, readerState.value.pageIndex + 3)
			)

			// Include as hidden pages the two previous and next pages
			return {
				all,
				hidden: all.filter(p => p !== pages[readerState.value.pageIndex])
			}
		} else if (readerState.value.mode === 'longstrip') {
			return {
				all: pages.slice(0, readerState.value.pageIndex + 2),
				hidden: []
			}
		} else {
			return {
				all: [],
				hidden: []
			}
		}
	})
}
