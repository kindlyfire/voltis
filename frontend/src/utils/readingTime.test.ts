import { describe, expect, it } from 'vitest'
import type { ContentLength } from './api/types'
import { formatDuration, lengthSummary, readingSpeed } from './readingTime'

describe('formatDuration', () => {
    it.each([
        [0.4, '< 1 min'],
        [45, '45 min'],
        [59.7, '1 h'],
        [62, '1 h'],
        [330, '5 h 30 min'],
        [725, '12 h'],
    ])('%s min → %s', (minutes, expected) => {
        expect(formatDuration(minutes)).toBe(expected)
    })
})

describe('lengthSummary', () => {
    const speed = readingSpeed()
    it.each<[string, ContentLength, string]>([
        [
            'words unread',
            { unit: 'words', total: 82000, remaining: 82000 },
            '82K words · 5 h 30 min',
        ],
        [
            'words in progress',
            { unit: 'words', total: 82000, remaining: 47500 },
            '82K words · 3 h 10 min left',
        ],
        [
            'grouped pages',
            { unit: 'pages', total: 1212, remaining: 1212 },
            '1,212 pages · 6 h 45 min',
        ],
        ['finished', { unit: 'pages', total: 212, remaining: 0 }, '212 pages · 1 h 10 min'],
    ])('%s', (_, length, expected) => {
        expect(lengthSummary(length, speed)).toBe(expected)
    })
})
