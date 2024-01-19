/**
 * Merges DiskCollection and DiskItem entries into Collection and Item entries.
 *
 * We essentially create Collections when none match the DiskCollection's contentUri, and
 * update the existing Collection when one does match. We do the same for Items.
 */

import consola from 'consola'
import { prisma } from '../database'

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
		const item = items.find(i => i.contentUri === ditem.contentUri)
		if (!item) {
			const collection = collections.find(
				c => c.contentUri === stripLastUriPart(ditem.contentUri)
			)
			if (!collection) {
				consola.warn('No collection found for item', ditem)
				continue
			}
			const v = await prisma.item.create({
				data: {
					contentUri: ditem.contentUri,
					name: ditem.name,
					coverPath: ditem.coverPath ?? '',
					metadata: {},
					type: ditem.contentUri.split(':')[0],
					collectionId: collection?.id
				}
			})
			items.push(v)
		}
	}
}

function stripLastUriPart(uri: string) {
	return uri.split(':').slice(0, -1).join(':')
}
