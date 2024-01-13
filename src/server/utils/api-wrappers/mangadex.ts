import { ExternalRequestError, fetchJson } from '../fetch'

type LocalizedString = Record<string, string>

export interface MangadexMangaTag {
	id: string
	type: 'tag'
	attributes: {
		name: LocalizedString
		description: LocalizedString
		group: string
		version: number
	}
}

export interface MangadexManga {
	id: string
	type: 'manga'
	attributes: {
		title: LocalizedString
		description: LocalizedString
		altTitles: LocalizedString[]
		links: Record<string, string>
		originalLanguage: string
		lastVolume: string
		lastChapter: string
		publicationDemographic: string
		status: string
		year: number
		contentRating: string
		tags: MangadexMangaTag[]
		version: number
	}
	relationships: Array<{
		id: string
		type: string
		related: string
		attributes: Record<string, any>
	}>
}

export const Mangadex = {
	fetchMangaById,
	fetchMangaByName
}

async function fetchMangaById(id: string) {
	const { json } = await fetchJson<{
		result: 'ok'
		data: MangadexManga
	}>(
		'https://api.mangadex.org/manga/' +
			id +
			'?' +
			new URLSearchParams([
				['includes[]', 'author'],
				['includes[]', 'artist']
			]).toString()
	)
	return json.data
}

async function fetchMangaByName(name: string): Promise<MangadexManga | null> {
	const { json } = await fetchJson<{
		result: 'ok'
		data: MangadexManga[]
	}>(
		'https://api.mangadex.org/manga?' +
			new URLSearchParams([
				['title', name],
				['order[relevance]', 'desc'],
				['includes[]', 'author'],
				['includes[]', 'artist']
			]).toString()
	)
	return json.data[0] ?? null
}
