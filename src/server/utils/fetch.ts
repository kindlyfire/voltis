export async function fetchJson<T = any>(input: string, init?: RequestInit) {
	const res = await fetch(input, init)
	return handleFetchResponse<T>(res)
}

async function handleFetchResponse<T = any>(res: Response) {
	if (!res.ok) {
		throw new ExternalRequestError(res, await res.text().catch(() => null))
	}
	return {
		res,
		json: (await res.json()) as T
	}
}

export class ExternalRequestError extends Error {
	public data: {
		status: number
		statusText: string
		headers: Headers
		redirected: boolean
		type: ResponseType
		url: string
		body: any
	}

	constructor(data: Response, body?: any) {
		try {
			if (body) var json = JSON.parse(body)
		} catch (e) {}
		super(json?.error ?? json?.message ?? 'External request failed')
		this.data = {
			status: data.status,
			statusText: data.statusText,
			headers: data.headers,
			redirected: data.redirected,
			type: data.type,
			url: data.url,
			body
		}
	}
}
