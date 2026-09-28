import { describe, expect, it } from 'vitest'
import type { Library } from './api/types'
import { autoMatchOn, libraryAutoMatches, overrideCount, resolveSettings } from './librarySettings'

function library(auto_match: Record<string, boolean>, ...sources: Record<string, boolean>[]) {
    return {
        settings: { book_series_inference: 'off', auto_match },
        sources: sources.map((a, i) => ({ path_uri: `/s${i}`, settings: { auto_match: a } })),
    } as Library
}

const at = (lib: Library, ...paths: string[]) => {
    paths.forEach((p, i) => (lib.sources[i]!.path_uri = p))
    return lib
}

describe('librarySettings', () => {
    it('resolves a source over its library', () => {
        const lib = library({ a: true, b: false })
        expect(resolveSettings(lib.settings, { auto_match: { a: false, c: true } })).toEqual({
            book_series_inference: 'off',
            auto_match: { a: false, b: false, c: true },
        })
        expect(lib.settings.auto_match).toEqual({ a: true, b: false })
        expect(autoMatchOn(lib.settings, {}, 'a')).toBe(true)
        expect(autoMatchOn(lib.settings, {}, 'c')).toBe(false)
    })

    it('matches when the library or a source is on', () => {
        expect(libraryAutoMatches(library({}, { a: true }), ['a'])).toBe(true)
        expect(libraryAutoMatches(library({ a: true }, { a: false }), ['a'])).toBe(true)
        expect(libraryAutoMatches(library({ a: true }), ['b'])).toBe(false)
        expect(libraryAutoMatches(library({ a: false }, { b: true }, {}), ['a'])).toBe(false)
        // Files under "." are stored without a prefix, so the library value applies.
        for (const p of ['.', '', './', 'x/..', './x/../.']) {
            expect(libraryAutoMatches(at(library({}, { a: true }), p), ['a'])).toBe(false)
        }
        for (const p of ['x', '../x', '/']) {
            expect(libraryAutoMatches(at(library({}, { a: true }), p), ['a'])).toBe(true)
        }
    })

    it('counts overrides', () => {
        expect(
            overrideCount(library({}, { a: false }, {}, { a: true, b: true }).sources, 'a')
        ).toBe(2)
    })
})
