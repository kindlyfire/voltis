import { ExternalRequestError, fetchJson } from '../fetch'
import PQueue from 'p-queue'

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

const queue = new PQueue({
	concurrency: 5,
	interval: 1100,
	intervalCap: 5,
	carryoverConcurrencyCount: true
})

export const Mangadex = {
	fetchMangaById,
	fetchMangaByName
}

async function fetchMangaById(id: string) {
	const json = await queue
		.add(() =>
			fetchJson<{
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
		)
		.then(v => v!.json)
	return json.data
}

async function fetchMangaByName(name: string): Promise<MangadexManga | null> {
	const json = await queue
		.add(() =>
			fetchJson<{
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
		)
		.then(v => v!.json)
	return json.data[0] ?? null
}
