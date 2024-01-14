import { Collection, CollectionMetadataSource } from '../../models/collection'
import util from 'util'

export const mangadexMetadataFn = async (
	col: Collection,
	source_: CollectionMetadataSource
): Promise<CollectionMetadataSource> => {
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
			manga = await Mangadex.fetchMangaByName(col.nameOverride || col.name)
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
	source.updatedAt = new Date().toISOString()
	return source
}
