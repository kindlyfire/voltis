import sharp from 'sharp'
import { promiseAllSettled2 } from '../../utils/utils'
import { getComicData } from '../../utils/comic-reader'
import path from 'pathe'
import { DiskItem } from '@prisma/client'

export interface DiskItemComicMetadata {
	suggestedMode: 'pages' | 'longstrip'
	files: Array<{
		name: string
		width: number
		height: number
	}>
}

export const diskItemComicMetadataFn = async (
	item: DiskItem
): Promise<DiskItemComicMetadata> => {
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
		suggestedMode,
		files
	}
}
