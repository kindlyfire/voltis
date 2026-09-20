import { describe, expect, it } from 'vitest'
import { resolveEpubRef } from './epubPaths'

/** Mirrors backend/lib/epub/path_test.go — the two resolvers must agree. */
const RESOLVES: Array<[base: string, ref: string, href: string, fragment: string]> = [
    ['OPS/content.opf', 'text/ch1.xhtml', 'OPS/text/ch1.xhtml', ''],
    ['OPS/nav/nav.xhtml', '../text/ch1.xhtml#start', 'OPS/text/ch1.xhtml', 'start'],
    ['OPS/nav/nav.xhtml', './../text/./ch1.xhtml', 'OPS/text/ch1.xhtml', ''],
    ['content.opf', 'Text/Chapter%201.xhtml', 'Text/Chapter 1.xhtml', ''],
    ['OPS/content.opf', 'text/ch1.xhtml#caf%C3%A9', 'OPS/text/ch1.xhtml', 'café'],
    ['OPS/text/ch1.xhtml', '#note', 'OPS/text/ch1.xhtml', 'note'],
    ['OPS/content.opf', 'text/ch1.xhtml?v=2', 'OPS/text/ch1.xhtml', ''],
    ['OPS/content.opf', 'text/ch1.xhtml?v=2#x', 'OPS/text/ch1.xhtml', 'x'],
    ['OPS/content.opf', '  text/ch1.xhtml  ', 'OPS/text/ch1.xhtml', ''],
    ['OPS/content.opf', '100%.xhtml', 'OPS/100%.xhtml', ''],
    ['OPS/content.opf', 'Text/MiXeD.XHTML', 'OPS/Text/MiXeD.XHTML', ''],
    ['', 'images/pic.png', 'images/pic.png', ''],
    ['OPS/a/b/c.xhtml', '../../d.xhtml', 'OPS/d.xhtml', ''],
]

const REJECTS: Array<[base: string, ref: string]> = [
    ['OPS/content.opf', ''],
    ['OPS/content.opf', '   '],
    ['OPS/content.opf', '../../../etc/passwd'],
    ['content.opf', '../secret.txt'],
    ['OPS/content.opf', '..'],
    ['OPS/content.opf', '.'],
    ['OPS/content.opf', 'images/'],
    ['OPS/content.opf', '/etc/passwd'],
    ['OPS/content.opf', '//cdn.example.com/x.js'],
    ['OPS/content.opf', 'http://example.com/x.xhtml'],
    ['OPS/content.opf', 'HTTPS://example.com/x.xhtml'],
    ['OPS/content.opf', 'mailto:a@example.com'],
    ['OPS/content.opf', 'data:text/html,x'],
    ['OPS/content.opf', 'javascript:alert(1)'],
    ['OPS/content.opf', '..%2f..%2fsecret.txt'],
    ['OPS/content.opf', 'text%5Cch1.xhtml'],
    ['OPS/content.opf', 'text\\ch1.xhtml'],
    ['', '#fragment-only'],
]

describe('resolveEpubRef', () => {
    it.each(RESOLVES)('resolves %s + %s', (base, ref, href, fragment) => {
        expect(resolveEpubRef(base, ref)).toEqual({ href, fragment })
    })

    it.each(REJECTS)('rejects %s + %s', (base, ref) => {
        expect(resolveEpubRef(base, ref)).toBeNull()
    })
})
