import { Collection } from '../../models/collection'
import { Item, ItemMetadataSource } from '../../models/item'
import sharp from 'sharp'
import { promiseAllSettled2 } from '../../utils/utils'
import { getComicData } from '../../utils/comic-reader'
import path from 'pathe'

export interface FileMetadataCustomData {
	suggestedMode?: 'pages' | 'longstrip'
	files: Array<{
		name: string
		width: number
		height: number
	}>
}

export const fileMetadataFn = async (
	col: Collection,
	item: Item,
	source: ItemMetadataSource
): Promise<ItemMetadataSource> => {
	const comicData = await getComicData(item.id)

	const [files] = await promiseAllSettled2(
		comicData.files.map(async f => {
			const meta = await sharp(path.join(comicData.root, f)).metadata()
			return {
				name: f,
				width: meta.width ?? 1,
				height: meta.height ?? 1
			}
		})
	)

	let suggestedMode: 'pages' | 'longstrip' = 'pages'
	if (files.length > 1) {
		const first = files[0]
		const ratio = first.width / first.height
		if (ratio < 0.6) suggestedMode = 'longstrip'
	}

	return {
		...source,
		updatedAt: new Date().toISOString(),
		customData: <FileMetadataCustomData>{
			suggestedMode,
			files
		}
	}
}
