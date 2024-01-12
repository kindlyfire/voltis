import { getQuery } from 'h3'
import fs from 'fs-extra'
import { Item } from '../models/item'
import { Collection } from '../models/collection'

export default defineEventHandler(async event => {
	const query = getQuery(event)
	const itemId = query['item-id']
	const collectionId = query['collection-id']

	let coverPath = ''
	if (typeof itemId === 'string') {
		const item = await Item.findByPk(itemId)
		if (item?.coverPath) {
			coverPath = item.coverPath
		}
	} else if (typeof collectionId === 'string') {
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

	await sendStream(event, fs.createReadStream(coverPath))
})
