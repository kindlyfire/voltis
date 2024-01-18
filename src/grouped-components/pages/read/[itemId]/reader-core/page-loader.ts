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
