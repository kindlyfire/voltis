import { getQuery } from 'h3'
import fs from 'fs-extra'
import sharp from 'sharp'
import { z } from 'zod'
import { prisma } from '../database'

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
		const item = await prisma.item.findById(itemId)
		if (item?.coverPath) {
			coverPath = item.coverPath
		}
	} else if (collectionId) {
		const collection = await prisma.collection.findById(collectionId)
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

	const stat = await fs.stat(coverPath).catch(() => null)
	if (!stat) {
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
