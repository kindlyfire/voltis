import { getQuery } from 'h3'
import { getComicReader } from '../utils/comic-reader'

export default defineEventHandler(async event => {
	const query = getQuery(event)
	const itemId = query['item-id']
	const fileName = query['file-name']
	if (typeof itemId !== 'string' || typeof fileName !== 'string') {
		setResponseStatus(event, 400)
		return 'Bad Request'
	}

	const reader = await getComicReader(itemId).catch(() => null)
	if (!reader) {
		setResponseStatus(event, 404)
		return 'Not Found'
	}

	const file = reader.files.find(file => file.name === fileName)
	if (!file) {
		setResponseStatus(event, 404)
		return 'Not Found'
	}

	setHeader(event, 'Cache-Control', 'public, max-age=31536000')

	await sendStream(event, new Blob([file.data]).stream())
})
