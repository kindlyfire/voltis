import { type FileMetadataCustomData } from '../../../server/scanning/comic/metadata-file'
import {
	useCollection,
	useItem,
	useReaderData
} from '../../../state/composables/queries'

const PREFETCH_COUNT = 2
export const useComicReaderStore = () => {
	const itemId = ref(null as string | null)

	const qItem = useItem(itemId)
	const item = computed(() => qItem.data.value)

	const qCollection = useCollection(computed(() => item.value?.collectionId))
	const collection = computed(() => qCollection.data.value)

	const qReaderData = useReaderData(itemId)
	const readerData = computed(() => qReaderData.data.value)
	watch(
		() => readerData.value,
		rd => {
			if (rd) readerState.mode = rd.suggestedMode ?? 'pages'
		}
	)

	const readerState = reactive({
		pageIndex: 0,
		mode: null as 'pages' | 'longstrip' | null,
		pages: [] as Array<ReturnType<typeof createPageState>>
	})
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

	const readerPages = computed(() => {
		const pages = readerState.pages
		if (pages.length === 0) return []

		if (readerState.mode === 'pages') {
			const p = pages[readerState.pageIndex]
			p.fetch()
			return [p]
		} else if (readerState.mode === 'longstrip') {
			return pages.slice(0, readerState.pageIndex + 2)
		}
	})

	function switchPage(offset: 1 | -1) {
		readerState.pageIndex = Math.max(
			0,
			Math.min(readerState.pageIndex + offset, readerState.pages.length - 1)
		)
	}
	function switchMode() {
		readerState.mode = readerState.mode === 'pages' ? 'longstrip' : 'pages'
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
	}

	const state = reactive({
		file: f,
		error: null as string | null,
		blobUrl: null as string | null,
		fetch() {
			if (fetchPromise) return
			fetchPromise = doFetch().catch(e => {
				state.error = e.message ?? '' + e
			})
		}
	})

	return state
}
