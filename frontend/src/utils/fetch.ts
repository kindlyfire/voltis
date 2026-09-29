import type { ErrorResponse } from './api/types'

export const API_URL = import.meta.env.VITE_API_URL ?? '/api'

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
        if (!(error instanceof RequestError)) return String(error)
        const err = (error.json as ErrorResponse | undefined)?.error
        return typeof err === 'string' && err ? err : error.message
    }
}

/** Network failures (no response) and 5xx are worth retrying; 4xx and parse errors aren't. */
export function isTransientError(error: unknown): boolean {
    if (!(error instanceof RequestError)) return false
    return !error.response || error.response.status >= 500
}

export async function apiFetch<TData>(
    input: string,
    init?: RequestInit & {
        allowNotJson?: boolean
    }
) {
    const res = await apiFetchRaw<TData>(input, init)
    return res.data
}

export async function apiFetchRaw<TData>(
    input: string,
    init?: RequestInit & {
        allowNotJson?: boolean
    }
): Promise<{ data: TData; res: Response }> {
    const url = input.startsWith('http') ? input : `${API_URL}${input}`

    const headers = new Headers(init?.headers)
    if (typeof init?.body === 'string' && !headers.has('Content-Type')) {
        headers.set('Content-Type', 'application/json')
    }

    let res: Response
    try {
        res = await fetch(url, { ...init, headers, credentials: 'include' })
    } catch (err) {
        throw new RequestError(err instanceof Error ? err.message : String(err))
    }

    let text: string
    try {
        text = await res.text()
    } catch (err) {
        // No `response`: a body cut off mid-read is a network failure, so it's retried.
        throw new RequestError(err instanceof Error ? err.message : String(err))
    }
    let json: unknown
    let parsed = true
    try {
        json = JSON.parse(text)
    } catch {
        parsed = false
    }

    if (!res.ok) {
        // `statusText` is empty over HTTP/2, so it's left out. JSON bodies surface `json.error` anyway.
        const message =
            !parsed && res.status >= 500
                ? `Server error (${res.status})`
                : `Request failed (${res.status})`
        throw new RequestError(message, { response: res, json, text })
    }
    if (!parsed && !init?.allowNotJson) {
        throw new RequestError(`Response wasn't JSON: ${text.slice(0, 200)}`, {
            response: res,
            text,
        })
    }

    return { data: json as TData, res }
}
