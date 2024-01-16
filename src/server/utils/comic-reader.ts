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
		const files = await unzipStream(fs.createReadStream(this.item.path))
		this.files = files.sort((a, b) => a.name.localeCompare(b.name))
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

/**
 * Unzips a stream and returns an array of files and their data. It would
 * probably (1000%) be nicer to unzip to a temp directory with a native program.
 *
 * While this is slower than fflate's synchroneous unzip, it doesn't block the
 * event loop in the event of slow I/O (like a network mount)
 */
async function unzipStream(stream: fs.ReadStream) {
	const unzipper = new fflate.Unzip()
	unzipper.register(fflate.UnzipInflate)

	let files = [] as Array<{ name: string; data: Uint8Array }>
	const promises = [] as Promise<any>[]
	unzipper.onfile = f => {
		if (!['.png', '.jpg', '.jpeg'].includes(path.extname(f.name))) return
		const bufs = [] as Uint8Array[]
		const p = newUnpackedPromise()
		f.ondata = (e, d, final) => {
			bufs.push(d)
			if (final) {
				files.push({
					name: f.name,
					data: new Uint8Array(Buffer.concat(bufs))
				})
				p.resolve()
			}
		}
		promises.push(p.promise)
		f.start()
	}

	stream.on('data', (d: Buffer) => {
		unzipper.push(d)
	})
	stream.on('end', () => {
		unzipper.push(Buffer.alloc(0), true)
	})

	await new Promise((resolve, reject) => {
		stream.on('error', reject)
		stream.on('end', resolve)
	})
	await Promise.all(promises)
	return files
}
