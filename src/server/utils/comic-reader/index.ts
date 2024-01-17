import cluster from 'node:cluster'
import { getComicDataPrimary } from './cluster-primary'
import { ComicRequest, ComicResponse, isComicResponse } from './types'

export async function getComicData(itemId: string) {
	if (cluster.isPrimary) {
		return getComicDataPrimary(itemId)
	}

	const p = newUnpackedPromise<ComicResponse>()
	function listener(msg: any) {
		const timeout = setTimeout(() => {
			p.reject(new Error('Timeout waiting for comic to load'))
		}, 1000 * 60 * 1)
		if (isComicResponse(msg) && msg.itemId === itemId) {
			clearTimeout(timeout)
			if (msg.error) {
				p.reject(new Error(msg.error))
			} else {
				p.resolve(msg)
			}
		}
	}
	process.on('message', listener)
	process.send!(<ComicRequest>{ type: 'comic-request', itemId })
	const res = await p.promise.finally(() => process.off('message', listener))
	return res.data!
}
