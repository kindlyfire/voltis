import { describe, expect, it } from 'vitest'
import { redirectQuery, safeRedirect } from '@/utils/redirect'

describe('safeRedirect', () => {
    it.each([
        ['/x?a=1', '/x?a=1'],
        ['/', '/'],
        ['//evil', null],
        ['/..//evil', null],
        ['/\\evil', null],
        ['/\t/evil', null],
        ['https://evil', null],
        ['evil', null],
        ['/auth/login', null],
        ['/auth', null],
        ['/AUTH/login', null],
        ['', null],
        [undefined, null],
    ])('%j → %j', (value, want) => {
        expect(safeRedirect(value)).toBe(want)
        // `/` is the default target, so it's left out of the query.
        expect(redirectQuery(value)).toEqual(want && want !== '/' ? { redirect: want } : {})
    })
})
