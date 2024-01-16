import { getQuery } from 'h3'
import fs from 'fs-extra'
import { Item } from '../models/item'
import { Collection } from '../models/collection'
import sharp from 'sharp'
import { z } from 'zod'

const schema = z.object({
	'item-id': z.string().optional(),
	'collection-id': z.string().optional(),
	width: z.enum(['full', '320', '640']).default('full')
})

export default defineEventHandler(async event => {
	const query = getQuery(event)

	const {
		'item-id': itemId,
		'collection-id': collectionId,
		width
	} = schema.parse(query)

	let coverPath = ''
	if (itemId) {
		const item = await Item.findByPk(itemId)
		if (item?.coverPath) {
			coverPath = item.coverPath
		}
	} else if (collectionId) {
		const collection = await Collection.findByPk(collectionId)
		if (collection?.coverPath) {
			coverPath = collection.coverPath
		}
	} else {
		setResponseStatus(event, 400)
		return 'Bad Request'
	}

	if (!coverPath) {
		setResponseStatus(event, 404)
		return 'Not Found'
	}

	setHeader(event, 'Cache-Control', 'public, max-age=31536000')

	if (width !== 'full') {
		const transformer = sharp().resize(parseInt(width))
		await sendStream(event, fs.createReadStream(coverPath).pipe(transformer))
		return
	}

	await sendStream(event, fs.createReadStream(coverPath))
})
