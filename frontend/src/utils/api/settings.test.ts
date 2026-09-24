import { describe, expect, it } from 'vitest'
import { settingValue } from './settings'
import type { Setting } from './types'

function setting(key: string, value: Setting['value'], extra: Partial<Setting> = {}): Setting {
    return { key, type: 'string', value, secret: false, help: '', ...extra }
}

describe('settingValue', () => {
    const settings = [
        setting('a.string', 'hello'),
        setting('a.bool', false, { type: 'bool' }),
        setting('a.list', ['openid', 'email'], { type: 'string_list' }),
        setting('a.secret', null, { type: 'secret', secret: true, set: true }),
    ]

    it('returns stored values, including falsy ones', () => {
        expect(settingValue(settings, 'a.string', '')).toBe('hello')
        expect(settingValue(settings, 'a.bool', true)).toBe(false)
        expect(settingValue<string[]>(settings, 'a.list', [])).toEqual(['openid', 'email'])
    })

    it('falls back for unknown keys, missing data and redacted secrets', () => {
        expect(settingValue(settings, 'a.missing', 'fallback')).toBe('fallback')
        expect(settingValue(undefined, 'a.string', 'fallback')).toBe('fallback')
        expect(settingValue(settings, 'a.secret', '')).toBe('')
    })
})
