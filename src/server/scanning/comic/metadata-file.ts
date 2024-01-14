import { Collection } from '../../models/collection'
import { Item, ItemMetadataSource } from '../../models/item'
import sharp from 'sharp'
import { promiseAllSettled2 } from '../../utils/utils'
import { getComicReader } from '../../utils/comic-reader'

export interface FileMetadataCustomData {
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
	const reader = await getComicReader(item.id)

	const [files] = await promiseAllSettled2(
		reader.files.map(async f => {
			const meta = await sharp(f.data).metadata()
			return {
				name: f.name,
				width: meta.width,
				height: meta.height
			}
		})
	)

	return {
		...source,
		updatedAt: new Date().toISOString(),
		customData: <FileMetadataCustomData>{
			files
		}
	}
}
