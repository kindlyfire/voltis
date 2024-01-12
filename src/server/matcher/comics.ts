import consola from 'consola'
import { Matcher, MatcherItem } from '.'
import path from 'pathe'
import slugify from 'slugify'

const COVER_FILENAMES = ['cover.jpg', 'cover.png', 'cover.jpeg']
const CLEAN_FILENAME_RE = /((\[.+\])|(\(.+\))|(\{.+\})|\s)+$/g
export const comicsMatcher: Matcher = {
	checkIsCollection(dir, dirEntries) {
		const files = dirEntries
			.filter(i => i.isFile())
			.map(i => i.name.toLowerCase())
		const hasComic = files.some(f => f.endsWith('.cbz'))
		const cover = files.find(f => COVER_FILENAMES.includes(f))
		if (!hasComic) return null
		const cleanedName = path
			.parse(dir)
			.name.replaceAll(CLEAN_FILENAME_RE, '')
			.trim()
		return {
			contentId: slugify(cleanedName, {
				lower: true
			}),
			defaultName: cleanedName,
			coverPath: cover ? path.join(dir, cover) : null
		}
	},

	async updateCollection(col) {
		const metadata = await fetchMetadataFromMD(col.name)
		if (!metadata) return
		col.metadata = metadata
	},

	listItems(col, dirEntries) {
		const files = dirEntries.filter(
			f => f.isFile() && f.name.toLowerCase().endsWith('.cbz')
		)
		return files
			.map(f => {
				const cleanedName = path
					.parse(f.name)
					.name.replaceAll(CLEAN_FILENAME_RE, '')
					.trim()
				const contentId = extractNameContentId(cleanedName)
				return <MatcherItem>{
					contentId: contentId ?? '',
					defaultName:
						formatNameData(extractNameData(cleanedName)) || cleanedName,
					path: path.join(col.path, f.name)
				}
			})
			.filter(f => f.contentId)
	},

	updateItems(col, existingItems) {
		for (const item of existingItems) {
			const { volume, chapter } = extractNameData(item.name)
			item.sortValue = [volume ?? 1000000, chapter ?? 0]
		}
	}
}

const VOLUME_RE = /(v|volume|vol)\.?\s*([0-9]+(\.[0-9])?)/i
const CHAPTER_RE = /(c|chap|chapter)\.?\s*([0-9]+(\.[0-9])?)/i
const NUMBER_RE = /([0-9]+(\.[0-9])?)/i
function extractNameData(name: string) {
	let volume: number | null = null
	let chapter: number | null = null

	const _volume = name.match(VOLUME_RE)
	if (_volume && !isNaN(parseFloat(_volume[2]))) {
		volume = parseFloat(_volume[2])
	}
	const _chapter = name.match(CHAPTER_RE)
	if (_chapter && !isNaN(parseFloat(_chapter[2]))) {
		chapter = parseFloat(_chapter[2])
	} else if (volume === null) {
		const _number = name.match(NUMBER_RE)
		if (_number && !isNaN(parseFloat(_number[1]))) {
			chapter = parseFloat(_number[1])
		}
	}

	return {
		volume,
		chapter
	}
}

function extractNameContentId(name: string) {
	let { volume, chapter } = extractNameData(name)
	if (volume === null && chapter === null) {
		console.warn(`Could not extract content ID from ${name}`)
		return null
	}
	if (chapter === null) return `v${volume}`
	return `v${volume}:c${chapter}`
}

function formatNameData(data: ReturnType<typeof extractNameData>) {
	if (data.volume === null && data.chapter === null) return ''
	if (data.chapter === null) return `Volume ${data.volume}`
	if (data.volume === null) return `Chapter ${data.chapter}`
	return `Volume ${data.volume}, Chapter ${data.chapter}`
}

export interface Metadata {
	mangadexId: string | null
	description: string
	authors: string[]
	pubStatus: 'ongoing' | 'completed' | 'hiatus' | 'cancelled'
	pubYear: number | null
	altTitles: Array<{ [k: string]: string }>
}
async function fetchMetadataFromMD(name: string): Promise<Metadata | null> {
	consola.log('Fetching metadata for', name)
	const res = await fetch(
		'https://api.mangadex.org/manga?' +
			new URLSearchParams([
				['title', name],
				['order[relevance]', 'desc'],
				['includes[]', 'author'],
				['includes[]', 'artist']
			]).toString()
	)
	if (!res.ok) {
		consola.error('Failed to fetch metadata for', name)
		return null
	}

	const json = await res.json()
	if (json.data.length === 0) {
		consola.error('No entries found on MD for', name)
		return null
	}

	const data = json.data[0]

	let authors = data.relationships
		.filter((r: any) => r.type === 'author' || r.type === 'artist')
		.map((r: any) => r.attributes.name as string)
	authors = [...new Set(authors)]

	return {
		mangadexId: data.id,
		description: data.attributes.description?.en ?? '',
		authors,
		pubStatus: data.attributes.status,
		pubYear:
			typeof data.attributes.year === 'number' &&
			data.attributes.year.toString().length === 4
				? data.attributes.year
				: null,
		altTitles: [data.attributes.title, ...data.attributes.altTitles]
	}
}
