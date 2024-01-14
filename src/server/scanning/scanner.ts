import consola from 'consola'
import { Library } from '../models/library'
import fs from 'fs-extra'
import path from 'pathe'
import { Collection } from '../models/collection'
import { Item } from '../models/item'
import { Op } from 'sequelize'
import { comicMatcher } from '../scanning/comic'
import { MatcherCollection } from '../scanning'

export async function scanLibrary(lib: Library) {
	consola.log('Scanning', lib.name)

	const matchedCollections: (MatcherCollection & { path: string })[] = []
	await Promise.all(
		lib.paths.map(async libPath => {
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

	const collections = await Collection.findAll({
		where: {
			libraryId: lib.id
		}
	})
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
		const itemContentIds = await comicMatcher.listItems(
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
			const item = items.find(
				i => i.collectionId === col.id && i.contentId === matched.contentId
			)
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
						metadata: {
							sources: []
						},
						sortValue: []
					})
				)
			}
		}

		consola.log('Updating collection', col.name)
		await comicMatcher.updateCollection(col)
		await col.save()

		consola.log('Updating items', col.name)
		const _items = items.filter(i => i.collectionId === col.id)
		await comicMatcher.updateItems(col, _items)
		await Promise.all(_items.map(i => i.save()))
	}
}
