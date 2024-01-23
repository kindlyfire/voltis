import { defineClusterFn } from '../cluster-rpc'
import cluster from 'node:cluster'
import pMemoize from 'p-memoize'
import TTLCache from '@isaacs/ttlcache'
import os from 'node:os'
import path from 'pathe'
import fs from 'fs-extra'
import { execa } from 'execa'
import { globby } from 'globby'
import { prisma } from '../../database'

export const getComicData = defineClusterFn({
	name: 'getComicData',
	async fn(itemId: string) {
		console.log('is master?', cluster.isPrimary, itemId)
		return await getComicDataPrimary(itemId)
	}
})

const cache = new TTLCache<any, ComicData>({
	ttl: 1000 * 60 * 5, // keep around for 5 minutes
	max: 100,
	updateAgeOnGet: true,
	dispose: value => {
		fs.rm(value.root, { recursive: true }).catch(e => {
			console.error('Failed to dispose of cached comic files:', e)
		})
	}
})

const getComicDataPrimary = pMemoize(_getComicData, {
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

	// Sort the files alphabetically
	files.sort((a, b) => {
		return a.localeCompare(b)
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
