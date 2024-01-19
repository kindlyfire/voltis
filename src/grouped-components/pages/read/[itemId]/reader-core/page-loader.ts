import type { ChapterData } from './types'

export type PageLoader = ReturnType<typeof getPageLoader>

export function getPageLoaders(cd: ChapterData) {
	return cd.pages.map(p => getPageLoader(p))
}

function getPageLoader(page: ChapterData['pages'][0]) {
	let fetchPromise: Promise<void> | null = null
	async function doFetch() {
		const res = await fetch(page.url)
		if (!res.ok) {
			throw new Error('Failed to fetch page')
		}
		const blob = await res.blob()
		state.blobUrl = URL.createObjectURL(blob)
		state.error = null
		fetchPromise = null
	}

	const state = reactive({
		page,
		error: null as string | null,
		blobUrl: null as string | null,
		fetch() {
			if (fetchPromise || state.blobUrl)
				return fetchPromise ?? Promise.resolve()
			fetchPromise = doFetch().catch(e => {
				console.error('Failed to fetch page', e)
				state.error = e.message ?? '' + e
				fetchPromise = null
			})
			return fetchPromise
		}
	})

	return state
}

export function getPagesInPreloadOrder(pages: PageLoader[], pageIndex: number) {
	const pagesInPreloadOrder: PageLoader[] = []
	for (let i = 0; i < pages.length; i++) {
		const index1 = pageIndex + i
		const index2 = pageIndex - i
		if (index1 < pages.length) {
			pagesInPreloadOrder.push(pages[index1])
		}
		if (index2 !== index1 && index2 >= 0 && index2 < pages.length) {
			pagesInPreloadOrder.push(pages[index2])
		}
	}
	return pagesInPreloadOrder
}

export function preloadPages(pages: PageLoader[], concurrency: number) {
	for (
		let i = 0, preloading = 0;
		i < pages.length && preloading < concurrency;
		i++
	) {
		const page = pages[i]
		if (page.blobUrl) {
			continue
		}
		page.fetch()
		preloading++
	}
}
