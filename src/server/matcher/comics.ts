import { Matcher, MatcherItem } from '.'
import path from 'pathe'
import slugify from 'slugify'
import { Collection, CollectionMetadataSource } from '../models/collection'
import { Mangadex, MangadexManga } from '../utils/api-wrappers/mangadex'
import util from 'util'

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
		let source = col.metadata.sources.find(s => s.name === 'mangadex')
		if (!source) {
			source = {
				name: 'mangadex',
				data: {},
				updatedAt: null,
				remoteId: null
			}
		}
		source = await updateMangadexMetadata(col, source)
		col.metadata = {
			...col.metadata,
			sources: [
				source,
				...col.metadata.sources.filter(s => s.name !== 'mangadex')
			]
		}
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

async function updateMangadexMetadata(
	col: Collection,
	source_: CollectionMetadataSource
): Promise<CollectionMetadataSource> {
	const source: CollectionMetadataSource = {
		...source_,
		error: undefined
	}
	try {
		let manga: MangadexManga | null
		const id = source.overrideRemoteId || source.remoteId
		if (id) {
			manga = await Mangadex.fetchMangaById(id)
		} else {
			manga = await Mangadex.fetchMangaByName(col.name)
		}
		if (!manga) return source
		source.remoteId = manga.id
		source.data = {
			description:
				manga.attributes.description.en ??
				Object.entries(manga.attributes.description)[0]?.[1],
			authors: manga.relationships
				.filter(r => r.type === 'author')
				.map(r => r.attributes.name),
			pubStatus: manga.attributes.status as any,
			pubYear:
				typeof manga.attributes.year === 'number' &&
				manga.attributes.year.toString().length === 4
					? manga.attributes.year
					: null,
			titles: [manga.attributes.title, ...manga.attributes.altTitles]
		}
	} catch (e: any) {
		if (e instanceof Error) {
			source.error = {
				name: e.name,
				message: e.message,
				stack: e.stack
			}
		} else {
			source.error = {
				name: 'UnknownError',
				message: util.inspect(e)
			}
		}
	}
	return source
}
