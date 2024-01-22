import { getQuery } from 'h3'
import { getComicData } from '../utils/comic-reader'
import fs from 'fs-extra'
import path from 'pathe'

export default defineEventHandler(async event => {
	const query = getQuery(event)
	const ditemId = query['ditem-id']
	const fileName = query['file-name']
	if (typeof ditemId !== 'string' || typeof fileName !== 'string') {
		setResponseStatus(event, 400)
		return 'Bad Request'
	}

	const comicData = await getComicData(ditemId).catch(() => null)
	if (!comicData) {
		setResponseStatus(event, 404)
		return 'Not Found'
	}

	const file = comicData.files.find(f => f === fileName)
	if (!file) {
		setResponseStatus(event, 404)
		return 'Not Found'
	}

	const fullPath = path.join(comicData.root, file)
	const stat = await fs.stat(fullPath).catch(() => null)
	if (!stat) {
		setResponseStatus(event, 404)
		return 'Not Found'
	}

	setHeader(event, 'Cache-Control', 'public, max-age=31536000')

	await sendStream(event, fs.createReadStream(fullPath))
})
