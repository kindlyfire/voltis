import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiFetch, isTransientError, RequestError } from '@/utils/fetch'

const requestError = (json?: unknown) =>
    new RequestError('Request failed: 401 Unauthorized', { json })

describe('RequestError.getMessage', () => {
    it('returns the backend error message', () => {
        expect(RequestError.getMessage(requestError({ error: 'invalid credentials' }))).toBe(
            'invalid credentials'
        )
    })

    it.each([
        ['missing error', {}],
        ['missing body', undefined],
        ['non-string error', { error: { code: 1 } }],
        ['empty error', { error: '' }],
    ])('falls back for %s', (_, json) => {
        const error = requestError(json)
        expect(RequestError.getMessage(error)).toBe(error.message)
    })

    it('stringifies non-RequestError values', () => {
        expect(RequestError.getMessage(new Error('boom'))).toBe('Error: boom')
        expect(RequestError.getMessage('nope')).toBe('nope')
    })
})

describe('isTransientError', () => {
    it.each([
        ['no response', new RequestError('offline'), true],
        ['502', new RequestError('x', { response: new Response(null, { status: 502 }) }), true],
        ['404', new RequestError('x', { response: new Response(null, { status: 404 }) }), false],
        ['a plain Error', new Error(), false],
    ])('%s', (_, error, want) => {
        expect(isTransientError(error)).toBe(want)
    })
})

describe('apiFetch errors', () => {
    afterEach(() => vi.unstubAllGlobals())

    const respond = (body: string, status: number) =>
        vi.stubGlobal(
            'fetch',
            vi.fn(async () => new Response(body, { status }))
        )

    it('reports a non-JSON 5xx as a server error', async () => {
        respond('<html>Bad Gateway</html>', 502)
        const error = await apiFetch<never>('/x').catch((e: RequestError) => e)
        expect(error).toBeInstanceOf(RequestError)
        expect(error.message).toBe('Server error (502)')
        expect(RequestError.getMessage(error)).toBe('Server error (502)')
    })

    it('surfaces the JSON error of a 4xx', async () => {
        respond('{"error":"bad name"}', 400)
        const error = await apiFetch<never>('/x').catch((e: RequestError) => e)
        expect(RequestError.getMessage(error)).toBe('bad name')
        expect(error.response?.status).toBe(400)
    })
})
