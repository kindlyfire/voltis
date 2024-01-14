import * as fflate from 'fflate'
import fs from 'fs-extra'
import path from 'pathe'
import { Collection } from '../models/collection'
import { Item } from '../models/item'
import pMemoize from 'p-memoize'
import TTLCache from '@isaacs/ttlcache'

export class ComicReader {
	files = [] as Array<{ name: string; data: Uint8Array }>

	constructor(public col: Collection, public item: Item) {
		if (col.kind !== 'comic') {
			throw new Error('ComicReader can only be used with comic collections')
		}
	}

	async load() {
		const buf = await fs.readFile(this.item.path)
		// TODO: Switch to async
		const decompressed = fflate.unzipSync(new Uint8Array(buf), {
			filter(file) {
				return ['.png', '.jpg', '.jpeg'].includes(path.extname(file.name))
			}
		})
		this.files = Object.entries(decompressed)
			.map(([name, data]) => ({
				name,
				data
			}))
			.sort((a, b) => a.name.localeCompare(b.name))
	}
}

async function getComicReaderUncached(id: string) {
	const item = await Item.findByPk(id, {
		include: {
			association: Item.associations.collection,
			required: true
		}
	})
	if (!item) {
		throw new Error(`Item ${id} not found`)
	}
	const comicReader = new ComicReader(item.collection!, item)
	await comicReader.load()
	return comicReader
}

const cache = new TTLCache({
	ttl: 1000 * 60 * 1, // keep around for 10 minutes
	max: 100,
	updateAgeOnGet: true
})
export const getComicReader = pMemoize(getComicReaderUncached, {
	cacheKey: ([id]) => id,
	cache
})
