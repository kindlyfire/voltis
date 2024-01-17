export interface ComicResponse {
	type: 'comic-response'
	itemId: string
	error?: string
	data?: {
		root: string
		files: string[]
	}
}
export function isComicResponse(msg: any): msg is ComicResponse {
	return msg?.type === 'comic-response'
}

export interface ComicRequest {
	type: 'comic-request'
	itemId: string
}
export function isComicRequest(msg: any): msg is ComicRequest {
	return msg?.type === 'comic-request'
}
