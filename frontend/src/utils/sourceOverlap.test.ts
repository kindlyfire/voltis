import { describe, expect, it } from 'vitest'
import { describeOverlap, findOverlaps, overlapRelation } from './sourceOverlap'

describe('overlapRelation', () => {
    it('compares whole segments', () => {
        expect(overlapRelation('/media/books', '/media/books')).toBe('equal')
        expect(overlapRelation('/media/books/a', '/media/books')).toBe('inside')
        expect(overlapRelation('/media', '/media/books')).toBe('contains')
        expect(overlapRelation('/books', '/bookshelf')).toBeNull()
        expect(overlapRelation('/bookshelf', '/books')).toBeNull()
        expect(overlapRelation('/media/a', '/media/b')).toBeNull()
        expect(overlapRelation('/Books', '/books')).toBeNull()
    })

    it('treats / as containing everything', () => {
        expect(overlapRelation('/', '/')).toBe('equal')
        expect(overlapRelation('/', '/media')).toBe('contains')
        expect(overlapRelation('/media', '/')).toBe('inside')
    })
})

describe('findOverlaps', () => {
    it('describes each overlapping source', () => {
        const overlaps = findOverlaps('/media/books', [
            { path: '/media/books', label: 'Comics' },
            { path: '/media', label: 'Everything' },
            { path: '/media/books/manga', label: 'Manga' },
            { path: '/media/bookshelf', label: 'Shelf' },
        ])
        expect(overlaps.map(describeOverlap)).toEqual([
            { short: 'In use by Comics', long: 'Already used by Comics' },
            { short: 'Inside Everything', long: 'Inside /media, used by Everything' },
            { short: 'Contains Manga', long: 'Contains /media/books/manga, used by Manga' },
        ])
    })
})
