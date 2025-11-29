export const API_URL = import.meta.env.VITE_API_URL ?? ''

export class RequestError extends Error {
	response?: Response
	json?: unknown
	text?: string

	constructor(message: string, options?: { response?: Response; json?: unknown; text?: string }) {
		super(message)
		this.name = 'RequestError'
		this.response = options?.response
		this.json = options?.json
		this.text = options?.text
	}

	static getMessage(error: unknown): string {
		if (!(error instanceof RequestError) || !error.json || !(error.json as any).detail) {
			return String(error)
		}

		const json = error.json as Record<string, unknown>
		if (typeof json.detail === 'string') {
			return json.detail
		}
		if (Array.isArray(json.detail)) {
			return json.detail
				.map(d => {
					if (typeof d === 'object' && d !== null) {
						if (d.loc && d.msg) {
							return `${(d.loc as string[]).join('.')}: ${d.msg}`
						}
						return JSON.stringify(d)
					}
					return String(d)
				})
				.join(', ')
		}
		return JSON.stringify(json.detail)
	}
}

export async function apiFetch<TData>(
	input: string,
	init?: RequestInit
): Promise<{ data: TData; res: Response }> {
	const url = input.startsWith('http') ? input : `${API_URL}${input}`

	let res: Response
	try {
		res = await fetch(url, init)
	} catch (err) {
		throw new RequestError(err instanceof Error ? err.message : String(err))
	}

	let text: string | undefined
	let json: unknown

	try {
		text = await res.text()
		json = JSON.parse(text)
	} catch {
		// Response wasn't JSON
	}

	if (!res.ok) {
		throw new RequestError(`Request failed: ${res.status} ${res.statusText}`, {
			response: res,
			json,
			text,
		})
	}

	return { data: json as TData, res }
}
