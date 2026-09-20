import { describe, expect, it } from 'vitest'
import { RequestError } from '@/utils/fetch'

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
