import { describe, expect, it } from 'vitest'
import { zBookSettings } from './bookSettings'

describe('book settings', () => {
    it('defaults to paged with one column', () => {
        expect(zBookSettings.parse({})).toMatchObject({ mode: 'paged', spread: '1' })
    })

    it('replaces only the fields that are invalid', () => {
        expect(zBookSettings.parse({ fontSize: 1.5, mode: 'columns', spread: '2' })).toMatchObject({
            fontSize: 1.5,
            mode: 'paged',
            spread: '2',
        })
        expect(zBookSettings.parse({ mode: 'scroll', spread: 3 })).toMatchObject({
            mode: 'scroll',
            spread: '1',
        })
    })
})
