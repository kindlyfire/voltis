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

    let text: string | undefined
    let json: unknown

    try {
        text = await res.text()
        json = JSON.parse(text)
    } catch {
        if (!init?.allowNotJson) {
            throw new RequestError(`Response wasn't JSON: ${text}`, {
                response: res,
                text,
            })
        }
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
