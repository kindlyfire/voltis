import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { defineComponent, ref } from 'vue'
import { keysOwnedElsewhere, navDrawerOpen, useOverlayLayer } from './overlay'

function focusIn(html: string) {
    document.body.innerHTML = html
    document.querySelector<HTMLElement>('[data-focus]')!.focus()
}

function withLayer(kind: Parameters<typeof useOverlayLayer>[0]) {
    let isTop!: () => boolean
    const wrapper = mount(
        defineComponent({
            setup() {
                isTop = useOverlayLayer(kind, ref(true)).isTop
                return () => null
            },
        })
    )
    return Object.assign(wrapper, { isTop })
}

afterEach(() => {
    document.body.innerHTML = ''
})

describe('useOverlayLayer', () => {
    it('keeps the main-nav drawer above a page drawer opened after it, and dialogs above both', () => {
        const nav = withLayer('nav-drawer')
        const reader = withLayer('drawer')
        expect(nav.isTop()).toBe(true)
        expect(reader.isTop()).toBe(false)
        expect(navDrawerOpen.value).toBe(true)
        const dialog = withLayer('dialog')
        expect(dialog.isTop()).toBe(true)
        ;[dialog, reader, nav].forEach(w => w.unmount())
        expect(navDrawerOpen.value).toBe(false)
    })
})

describe('keysOwnedElsewhere', () => {
    it('lets the reader have keys when focus is on the page, even with a drawer open', () => {
        const drawer = withLayer('drawer')
        expect(keysOwnedElsewhere(new KeyboardEvent('keydown', { key: 'ArrowRight' }))).toBe(false)
        drawer.unmount()
    })

    it('ignores keys while a dialog or menu is open', () => {
        const dialog = withLayer('dialog')
        expect(keysOwnedElsewhere()).toBe(true)
        dialog.unmount()
        expect(keysOwnedElsewhere()).toBe(false)
    })

    it('lets the reader have keys when focus is on an open drawer panel itself', () => {
        focusIn('<div role="dialog" class="a-drawer" tabindex="-1" data-focus></div>')
        expect(keysOwnedElsewhere()).toBe(false)
    })

    it.each([
        ['a text field', '<input data-focus />'],
        [
            'a control inside a drawer',
            '<div role="dialog" class="a-drawer"><button data-focus>Close</button></div>',
        ],
        ['a slider', '<div role="slider" tabindex="0" data-focus></div>'],
        ['an opted-out region', '<div data-reader-ignore-keys><button data-focus></button></div>'],
    ])('ignores keys when focus is in %s', (_, html) => {
        focusIn(html)
        expect(keysOwnedElsewhere()).toBe(true)
    })

    it('ignores keys another handler already took', () => {
        const e = new KeyboardEvent('keydown', { key: 'ArrowRight', cancelable: true })
        e.preventDefault()
        expect(keysOwnedElsewhere(e)).toBe(true)
    })
})
