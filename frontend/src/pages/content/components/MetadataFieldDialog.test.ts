import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { FieldDef } from '@/utils/api/metadata'
import { addOverlays, settle } from '@/utils/modalTesting'
import MetadataFieldDialog from './MetadataFieldDialog.vue'

const staffDef: FieldDef = {
    key: 'staff',
    label: 'Staff',
    type: 'staff',
    options: ['writer', 'artist'],
    content_types: ['comic_series'],
    editable: true,
}

let wrapper: ReturnType<typeof mount<typeof MetadataFieldDialog>>

function button(label: string) {
    const el = [...document.querySelectorAll<HTMLElement>('button')].find(
        b => b.getAttribute('aria-label') === label || b.textContent?.trim() === label
    )
    if (!el) throw new Error(`no button ${label}`)
    return el
}

function field(label: string) {
    const el = [...document.querySelectorAll('label')].find(l => l.textContent?.trim() === label)
    if (!el) throw new Error(`no field ${label}`)
    return document.getElementById(el.htmlFor) as HTMLInputElement
}

function type(input: HTMLInputElement, value: string) {
    input.value = value
    input.dispatchEvent(new Event('input'))
}

async function show(def: FieldDef, value: unknown) {
    wrapper = mount(MetadataFieldDialog, {
        attachTo: document.body,
        props: {
            open: false,
            def,
            value,
            'onUpdate:open': (open: boolean) => wrapper.setProps({ open }),
        },
        global: { stubs: { ATooltip: { template: '<slot />' } } },
    })
    await wrapper.setProps({ open: true })
    await settle()
    // Vue drops a click whose handler was attached at the same instant, as all are on a frozen clock.
    vi.advanceTimersByTime(1)
}

beforeEach(() => {
    vi.useFakeTimers()
    addOverlays()
})

afterEach(() => {
    wrapper.unmount()
    vi.useRealTimers()
    document.body.innerHTML = ''
})

describe('MetadataFieldDialog', () => {
    it('edits existing staff as plain copies', async () => {
        const staff = [{ name: 'Ann', role: 'writer' }]
        await show(staffDef, staff)

        type(field('Name'), 'Ann B')
        button('OK').click()
        await settle()

        expect(wrapper.emitted('confirm')).toEqual([[[{ name: 'Ann B', role: 'writer' }]]])
        expect(staff).toEqual([{ name: 'Ann', role: 'writer' }])
        expect(wrapper.props('open')).toBe(false)
    })

    it('discards a cancelled draft, starting again from the value', async () => {
        await show(staffDef, [{ name: 'Ann', role: 'writer' }])
        type(field('Name'), 'Discarded')
        button('Cancel').click()
        await settle()
        expect(wrapper.emitted('confirm')).toBeUndefined()

        await wrapper.setProps({ open: true })
        await settle()
        expect(field('Name').value).toBe('Ann')
    })

    it('refuses invalid numbers and dates', async () => {
        const def: FieldDef = { ...staffDef, key: 'rating', label: 'Rating', type: 'int', max: 100 }
        await show(def, 150)
        button('OK').click()
        await settle()
        expect(document.body.textContent).toContain('Enter a number from … to 100')
        expect(wrapper.emitted('confirm')).toBeUndefined()

        await wrapper.setProps({
            open: false,
            def: { ...def, key: 'publication_date', label: 'Date', type: 'date' },
            value: '2020',
        })
        await wrapper.setProps({ open: true })
        await settle()
        type(field('Date'), '20-1')
        await settle()
        expect(document.body.textContent).toContain('Enter YYYY, YYYY-MM, or YYYY-MM-DD')
        type(field('Date'), ' 2020-05 ')
        button('OK').click()
        await settle()
        expect(wrapper.emitted('confirm')).toEqual([['2020-05']])
    })
})
