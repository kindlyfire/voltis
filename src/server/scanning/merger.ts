/**
 * Merges DiskCollection and DiskItem entries into Collection and Item entries.
 *
 * We essentially create Collections when none match the DiskCollection's contentUri, and
 * update the existing Collection when one does match. We do the same for Items.
 */

import consola from 'consola'
import { prisma } from '../database'
import { dbUtils } from '../database/utils'
import { comicMatcher } from './comic'

export async function mergeCollections() {
	const [diskCollections, collections, diskItems, items] = await Promise.all([
		prisma.diskCollection.findMany(),
		prisma.collection.findMany(),
		prisma.diskItem.findMany(),
		prisma.item.findMany()
	])

	// Create missing collections
	for (const dcol of diskCollections) {
		const uri = dcol.contentUriOverride || dcol.contentUri
		const col = collections.find(c => c.contentUri === uri)
		if (!col) {
			const v = await prisma.collection.create({
				data: {
					id: dbUtils.createId(),
					contentUri: uri,
					name: dcol.name,
					coverPath: dcol.coverPath ?? '',
					metadata: {},
					type: uri.split(':')[0]
				}
			})
			collections.push(v)
		}
	}

	// TODO: Update existing collections

	// Create missing items
	for (const ditem of diskItems) {
		let item = items.find(i => i.contentUri === ditem.contentUri)
		if (!item) {
			const collection = collections.find(
				c => c.contentUri === stripLastUriPart(ditem.contentUri)
			)
			if (!collection) {
				consola.warn('No collection found for ditem', ditem)
				continue
			}
			item = await prisma.item.create({
				data: {
					id: dbUtils.createId(),
					contentUri: ditem.contentUri,
					name: ditem.name,
					coverPath: ditem.coverPath ?? '',
					metadata: {},
					type: ditem.contentUri.split(':')[0],
					collectionId: collection?.id
				}
			})
			items.push(item)
		}
	}

	// Update items data
	for (const item of items) {
		const collection = collections.find(c => c.id === item.collectionId)
		if (!collection) {
			consola.warn('No collection found for item', item)
			continue
		}
		const ditems = diskItems.filter(i => i.contentUri === item.contentUri)
		const dcols = diskCollections.filter(
			c =>
				(c.contentUriOverride || c.contentUri) ===
				stripLastUriPart(item.contentUri)
		)
		await comicMatcher.updateItems(collection, item, dcols, ditems)
		await prisma.item.update({
			where: { id: item.id },
			data: item
		})
	}
}

function stripLastUriPart(uri: string) {
	return uri.split(':').slice(0, -1).join(':')
}
