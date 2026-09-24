import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { BookStructure } from '@/utils/api/types'
import BookContents from './BookContents.vue'

const stubs = {
    ANavItem: {
        props: ['to', 'active', 'disabled'],
        computed: {
            href(): string {
                const to = (this as { to?: { path: string; query: Record<string, string> } }).to
                return to ? `${to.path}?${new URLSearchParams(to.query)}` : ''
            },
        },
        template: `<a :data-to="href" :data-active="String(!!active)"
            :data-disabled="String(!!disabled)"><slot name="prefix" /><slot /></a>`,
    },
}

const NESTED: BookStructure = {
    spine: [
        { href: 'a.xhtml', title: 'File A', linear: true, words: 10 },
        { href: 'b.xhtml', title: 'File B', linear: true, words: 10 },
    ],
    toc: [
        { id: 'part', title: 'Part One', depth: 0, href: null, fragment: '' },
        { id: 'c1', title: 'Chapter One', depth: 1, href: 'a.xhtml', fragment: '' },
        { id: 'c1a', title: 'Section', depth: 2, href: 'a.xhtml', fragment: 's1' },
        { id: 'c2', title: 'Chapter Two', depth: 0, href: 'b.xhtml', fragment: '' },
    ],
}

function render(props: Record<string, unknown>) {
    return mount(BookContents, {
        props: { contentId: 'c_1', structure: NESTED, ...props },
        global: { stubs },
    })
}

describe('BookContents', () => {
    it('renders every depth and links into the reader', () => {
        const wrapper = render({})
        const items = wrapper.findAll('a')
        expect(items).toHaveLength(4)
        expect(items[1]!.attributes('data-to')).toBe('/r/c_1?ch=a.xhtml')
        expect(items[2]!.attributes('data-to')).toBe('/r/c_1?ch=a.xhtml&frag=s1')
        expect(items[0]!.attributes('data-disabled')).toBe('true')
        expect(items[0]!.attributes('data-to')).toBe('')
    })

    it('highlights the active page and its aliases', () => {
        const wrapper = render({ entryPages: { c1: 0, c1a: 0, c2: 1 }, activePage: 0 })
        const active = wrapper.findAll('a').map(item => item.attributes('data-active'))
        expect(active).toEqual(['false', 'true', 'true', 'false'])
    })

    it('emits select so the drawer can close and flush', async () => {
        const wrapper = render({})
        await wrapper.findAll('a')[1]!.trigger('click')
        expect(wrapper.emitted('select')).toHaveLength(1)
    })

    it('falls back to book sections when no TOC entry is usable', () => {
        const wrapper = render({
            structure: {
                spine: NESTED.spine,
                toc: [{ id: 'x', title: 'Nowhere', depth: 0, href: 'gone.xhtml', fragment: '' }],
            },
        })
        expect(wrapper.text()).toContain('no usable table of contents')
        expect(wrapper.text()).toContain('Book sections')
        expect(wrapper.findAll('a')).toHaveLength(2)
        expect(wrapper.findAll('a')[1]!.text()).toBe('2 File B')
    })

    it('takes the fallback override when targets failed to resolve', () => {
        const wrapper = render({ fallback: true })
        expect(wrapper.text()).toContain('Book sections')
        expect(wrapper.findAll('a')[0]!.attributes('data-to')).toBe('/r/c_1?ch=a.xhtml')
    })
})
