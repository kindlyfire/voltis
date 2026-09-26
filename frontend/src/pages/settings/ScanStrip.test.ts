import { describe, expect, it } from 'vitest'
import { stripKeys } from '@/pages/settings/ScanStrip.vue'

const fresh = (id: string) => `${id}:new`

describe('stripKeys', () => {
    it('keys first-seen entries freshly', () => {
        expect(stripKeys({ ids: [], keys: [] }, ['a', 'b'], fresh)).toEqual(['a:new', 'b:new'])
    })

    it('keeps the keys of entries pushed back by new ones', () => {
        const prev = { ids: ['a', 'b'], keys: ['a:1', 'b:2'] }
        expect(stripKeys(prev, ['c', 'a', 'b'], fresh)).toEqual(['c:new', 'a:1', 'b:2'])
    })

    it('keeps the key of an entry that stays at the front', () => {
        const prev = { ids: ['a', 'b'], keys: ['a:1', 'b:2'] }
        expect(stripKeys(prev, ['a', 'b'], fresh)).toEqual(['a:1', 'b:2'])
    })

    it('rekeys an entry promoted to the front', () => {
        const prev = { ids: ['a', 'b', 'c'], keys: ['a:1', 'b:2', 'c:3'] }
        expect(stripKeys(prev, ['c', 'a', 'b'], fresh)).toEqual(['c:new', 'a:1', 'b:2'])
    })
})
