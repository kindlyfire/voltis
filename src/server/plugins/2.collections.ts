import consola from 'consola'
import { dbReady } from './1.sequelize'
import { comicsMatcher } from '../matcher/comics'
import fs from 'fs-extra'
import path from 'pathe'
import { MatcherCollection } from '../matcher'
import { Collection } from '../models/collection'
import { Item } from '../models/item'
import { Op } from 'sequelize'

export interface LibraryDefinition {
	id: string
	name: string
	matcher: 'comic'
	path: string
}

export const libraries: LibraryDefinition[] = [
	// {
	// 	id: 'comics',
	// 	name: 'Comics',
	// 	matcher: 'comic',
	// 	path: ''
	// }
]

export default defineNitroPlugin(async () => {
	await dbReady
	scanCollections()
		.catch(err => {
			consola.error(err)
		})
		.then(() => {
			consola.log('Finished scanning collections')
		})
})

async function scanCollections() {
	for (const lib of libraries) {
		consola.log('Scanning', lib.name)
		const directories = await fs
			.readdir(lib.path, { withFileTypes: true })
			.then(entries => entries.filter(d => d.isDirectory()))
			.catch(e => {
				consola.warn('Failed to read directory', lib.path, e)
				return []
			})

		const matchedCollections: (MatcherCollection & { path: string })[] = []
		for (const dir of directories) {
			const matchedCollection = await comicsMatcher.checkIsCollection(
				path.join(lib.path, dir.name),
				await fs.readdir(lib.path + '/' + dir.name, { withFileTypes: true })
			)
			if (matchedCollection)
				matchedCollections.push({
					...matchedCollection,
					path: path.join(lib.path, dir.name)
				})
		}

		const collections = await Collection.findAll()
		const missingCollections = collections.filter(
			c => !matchedCollections.some(m => m.contentId === c.contentId)
		)
		const otherCollections = matchedCollections.filter(
			m => !collections.some(c => c.contentId === m.contentId)
		)

		for (const col of missingCollections) {
			await col.update({
				missing: true
			})
		}
		for (const matched of otherCollections) {
			const col = collections.find(c => c.contentId === matched.contentId)
			if (col) {
				await col.update({
					missing: false,
					path: matched.path,
					name: matched.defaultName,
					coverPath: matched.coverPath ?? ''
				})
			} else {
				collections.push(
					await Collection.create({
						contentId: matched.contentId,
						name: matched.defaultName,
						path: matched.path,
						coverPath: matched.coverPath ?? '',
						kind: 'comic',
						libraryId: lib.id
					})
				)
			}
		}

		const items = await Item.findAll({
			where: {
				collectionId: {
					[Op.in]: collections.map(c => c.id)
				}
			}
		})
		for (const col of collections.filter(c => !c.missing)) {
			const itemContentIds = await comicsMatcher.listItems(
				col,
				await fs.readdir(col.path, { withFileTypes: true })
			)

			const missingItems = items.filter(
				i =>
					i.collectionId === col.id &&
					!itemContentIds.some(m => m.contentId === i.contentId)
			)
			const otherItems = itemContentIds.filter(
				m =>
					!items.some(
						i => i.collectionId === col.id && i.contentId === m.contentId
					)
			)

			for (const item of missingItems) {
				await item.destroy()
			}
			for (const matched of otherItems) {
				const item = items.find(i => i.contentId === matched.contentId)
				if (item) {
					await item.update({
						path: matched.path,
						coverPath: matched.coverPath || item.coverPath || ''
					})
				} else {
					items.push(
						await Item.create({
							collectionId: col.id,
							contentId: matched.contentId,
							name: matched.defaultName,
							path: matched.path,
							coverPath: matched.coverPath ?? '',
							altNames: [],
							metadata: {},
							sortValue: []
						})
					)
				}
			}

			consola.log('Updating collection', col.name)
			await comicsMatcher.updateCollection(col)
			await col.save()

			consola.log('Updating items', col.name)
			const _items = items.filter(i => i.collectionId === col.id)
			await comicsMatcher.updateItems(col, _items)
			await Promise.all(_items.map(i => i.save()))
		}
	}
}
