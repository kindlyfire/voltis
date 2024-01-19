import cluster from 'node:cluster'
import pMemoize from 'p-memoize'
import TTLCache from '@isaacs/ttlcache'
import { ComicResponse, isComicRequest } from './types'
import os from 'node:os'
import path from 'pathe'
import fs from 'fs-extra'
import { execa } from 'execa'
import { globby } from 'globby'
import { prisma } from '../../database'

if (cluster.isPrimary) {
	cluster.on('message', (worker, msg) => {
		if (isComicRequest(msg)) {
			getComicDataPrimary(msg.itemId)
				.then(data => {
					worker.send(<ComicResponse>{
						type: 'comic-response',
						itemId: msg.itemId,
						data
					})
				})
				.catch(e => {
					worker.send(<ComicResponse>{
						type: 'comic-response',
						itemId: msg.itemId,
						error: e.message
					})
				})
		}
	})
}

const cache = new TTLCache<any, ComicData>({
	ttl: 1000 * 60 * 5, // keep around for 5 minutes
	max: 100,
	updateAgeOnGet: true,
	dispose: value => {
		fs.rm(value.root, { recursive: true })
			.then(() => {})
			.catch(e => {
				console.error('Failed to dispose of cached comic files:', e)
			})
	}
})
export const getComicDataPrimary = pMemoize(_getComicData, {
	cacheKey: ([id]) => id,
	cache
})

type ComicData = Awaited<ReturnType<typeof _getComicData>>
async function _getComicData(itemId: string) {
	if (!cluster.isPrimary) {
		throw new Error('getComicData must be called on the primary cluster worker')
	}

	const item = await prisma.diskItem.findById(itemId)
	if (!item) throw new Error(`Item ${itemId} not found`)

	const dir = path.join(os.tmpdir(), 'voltis', itemId)
	await fs.mkdir(dir, {
		recursive: true
	})

	let files = await fs.readdir(dir)
	if (files.length === 0) {
		await unzipFileToFolder(item.path, dir)
	}

	// We list files again, but recursively
	files = await globby('**/*', {
		cwd: dir,
		onlyFiles: true
	})

	return {
		root: dir,
		files
	}
}

async function unzipFileToFolder(file: string, folder: string) {
	try {
		if (process.platform === 'win32') {
			// Windows tar has zip support ( https://superuser.com/a/1473255 )
			// and unlike Expand-Archive, it doesn't require the .zip extension
			await execa('tar', ['-xf', `"${file}"`, '-C', `"${folder}"`], {
				shell: 'powershell.exe'
			})
		} else {
			await execa('unzip', [file, '-d', folder])
		}
	} catch (e) {
		console.error('error unzipping', e)
		throw new Error('Error unzipping file')
	}
}
