import consola from 'consola'
import fs from 'fs-extra'
import path from 'pathe'
import { comicMatcher } from '../scanning/comic'
import { MatcherCollection } from '../scanning'
import { prisma } from '../database'
import { mergeCollections } from './merger'
import { dbUtils } from '../database/utils'
import { DataSource } from '@prisma/client'
import { TaskContext, taskRunner } from '../utils/task-runner'

export const scanDataSources = defineClusterFn({
	name: 'scanDataSources',
	fn: async (dataSources: DataSource[]) => {
		if (taskRunner.tasks.some(t => t.name === 'scanDataSource'))
			throw new Error('A scan is already ongoing')

		await taskRunner.run({
			name: 'scanDataSource',
			displayName: 'Scan data source' + (dataSources.length > 1 ? 's' : ''),
			async fn(ctx) {
				for (const dataSource of dataSources) {
					await _scanDataSource(ctx, dataSource)
				}
			}
		})
	}
})

async function _scanDataSource(ctx: TaskContext, dataSource: DataSource) {
	consola.log('Scanning', dataSource.name)

	const matchedCollections: (MatcherCollection & { path: string })[] = []
	await Promise.all(
		dataSource.paths.map(async libPath => {
			const directories = await fs
				.readdir(libPath, { withFileTypes: true })
				.then(entries => entries.filter(d => d.isDirectory()))
				.catch(e => {
					consola.warn('Failed to read directory', libPath, e)
					return []
				})

			for (const dir of directories) {
				const matchedCollection = await comicMatcher.checkIsCollection(
					path.join(libPath, dir.name),
					await fs.readdir(libPath + '/' + dir.name, { withFileTypes: true })
				)
				if (matchedCollection)
					matchedCollections.push({
						...matchedCollection,
						path: path.join(libPath, dir.name)
					})
			}
		})
	)

	const collections = await prisma.diskCollection.findMany({
		where: { dataSourceId: dataSource.id }
	})
	const missingCollections = collections.filter(
		c => !matchedCollections.some(m => m.contentUri === c.contentUri)
	)
	const otherCollections = matchedCollections.filter(
		m => !collections.some(c => c.contentUri === m.contentUri)
	)

	for (const col of missingCollections) {
		await prisma.diskCollection.update({
			where: { id: col.id },
			data: { missing: true }
		})
	}
	for (const matched of otherCollections) {
		const col = collections.find(c => c.contentUri === matched.contentUri)
		if (col) {
			await prisma.diskCollection.update({
				where: { id: col.id },
				data: {
					missing: false,
					path: matched.path,
					name: matched.defaultName,
					coverPath: matched.coverPath ?? ''
				}
			})
		} else {
			const v = await prisma.diskCollection.create({
				data: {
					id: dbUtils.createId(),
					contentUri: matched.contentUri,
					name: matched.defaultName,
					path: matched.path,
					coverPath: matched.coverPath ?? '',
					dataSourceId: dataSource.id,
					type: 'comic'
				}
			})
			collections.push(v)
		}
	}

	const items = await prisma.diskItem.findMany({
		where: { DiskCollection: { dataSourceId: dataSource.id } }
	})
	for (const col of collections.filter(c => !c.missing)) {
		const itemContentIds = await comicMatcher.listItems(
			col,
			await fs.readdir(col.path, { withFileTypes: true })
		)

		const missingItems = items.filter(
			i =>
				i.diskCollectionId === col.id &&
				!itemContentIds.some(m => m.contentUri === i.contentUri)
		)
		const otherItems = itemContentIds.filter(
			m =>
				!items.some(
					i => i.diskCollectionId === col.id && i.contentUri === m.contentUri
				)
		)

		for (const item of missingItems) {
			await prisma.diskItem.delete({ where: { id: item.id } })
		}
		for (const matched of otherItems) {
			const item = items.find(
				i =>
					i.diskCollectionId === col.id && i.contentUri === matched.contentUri
			)
			if (item) {
				await prisma.diskItem.update({
					where: { id: item.id },
					data: {
						path: matched.path,
						coverPath: matched.coverPath ?? item.coverPath ?? ''
					}
				})
			} else {
				const v = await prisma.diskItem.create({
					data: {
						id: dbUtils.createId(),
						diskCollectionId: col.id,
						contentUri: matched.contentUri,
						name: matched.defaultName,
						path: matched.path,
						coverPath: matched.coverPath ?? '',
						metadata: {}
					}
				})
				items.push(v)
			}
		}

		consola.log('Updating collection', col.name)
		await comicMatcher.updateCollection(col)
		await prisma.diskCollection.update({
			where: { id: col.id },
			data: col
		})
	}

	consola.log('Merging collections and items')
	await mergeCollections()
}
