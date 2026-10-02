import { describe, expect, it } from 'vitest'
import { describeOverlaps, findOverlaps, overlapRelation } from './sourceOverlap'

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

describe('describeOverlaps', () => {
    it('groups overlaps by relation', () => {
        const overlaps = findOverlaps('/media/books', [
            { path: '/media/books', label: 'Comics', libraryId: '1' },
            { path: '/media/books', label: 'this library', sameLibrary: true },
            { path: '/media', label: 'Everything', libraryId: '2' },
            { path: '/media', label: 'this library', sameLibrary: true },
            { path: '/media/books/manga/a', label: 'Manga', libraryId: '3' },
            { path: '/media/books/manga/b', label: 'Manga', libraryId: '3' },
            { path: '/media/books/shelf', label: 'Shelf', libraryId: '4' },
            { path: '/media/books/new', label: 'this library', sameLibrary: true },
            { path: '/media/bookshelf', label: 'Shelf', libraryId: '4' },
        ])
        expect(describeOverlaps(overlaps)).toEqual({
            short: 'In use by Comics and this library · Inside Everything · Contains Manga and Shelf',
            long: 'Already used by Comics and this library. Inside a folder used by Everything. Contains folders used by Manga and Shelf.',
        })
        expect(describeOverlaps([])).toBeUndefined()
        expect(
            describeOverlaps(findOverlaps('/media', [{ path: '/media/books', label: 'Manga' }]))
        ).toMatchObject({ long: 'Contains a folder used by Manga.' })
    })
})
