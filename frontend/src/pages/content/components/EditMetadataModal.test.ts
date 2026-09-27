import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { MetadataView } from '@/utils/api/metadata'
import { apiFetch, RequestError } from '@/utils/fetch'
import { ModalContainer } from '@/utils/modals'
import { addOverlays, settle } from '@/utils/modalTesting'
import { showEditMetadataModal } from './EditMetadataModal.vue'

vi.mock('@/utils/fetch', async importOriginal => ({
    ...(await importOriginal<typeof import('@/utils/fetch')>()),
    apiFetch: vi.fn(),
}))

const config = {
    fields: [
        {
            key: 'title',
            label: 'Title',
            type: 'string',
            content_types: ['comic_series'],
            editable: true,
        },
        {
            key: 'cover',
            label: 'Cover',
            type: 'cover',
            content_types: ['comic_series'],
            editable: true,
        },
        {
            key: 'staff',
            label: 'Staff',
            type: 'staff',
            options: ['writer', 'artist'],
            content_types: ['comic_series'],
            editable: true,
        },
        {
            key: 'volume',
            label: 'Volume',
            type: 'string',
            content_types: ['comic'],
            editable: true,
        },
    ],
    providers: [],
}

const remoteCover = { url: 'https://fake/cover.jpg' }

function viewAt(rev: number, overrides: Record<string, unknown>): MetadataView {
    return {
        type: 'comic_series',
        overrides_rev: rev,
        merged: { title: 'Mine', cover: remoteCover, staff: [{ name: 'Ann', role: 'writer' }] },
        sources: { title: ['overrides'], cover: ['fake'], staff: ['file'] },
        layers: [
            {
                source: 'file',
                label: 'File',
                kind: 'file',
                fields: { title: 'Folder' },
            },
            {
                source: 'overrides',
                label: 'Overrides',
                kind: 'overrides',
                fields: overrides,
            },
        ],
        links: [],
    }
}

let view: MetadataView
let saves: { rev: number; fields: Record<string, unknown> }[]
let wrapper: ReturnType<typeof mount>

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

async function editStaffName(name: string) {
    button('Edit Staff').click()
    await settle()
    type(field('Name'), name)
    button('OK').click()
    await settle()
}

beforeEach(() => {
    vi.useFakeTimers()
    addOverlays()
    // A hidden key (volume is for comics) and a title override.
    view = viewAt(2, { title: 'Mine', volume: '3' })
    saves = []
    vi.mocked(apiFetch).mockImplementation(async (url, init) => {
        if (url === '/metadata/config') return config
        if (url === '/metadata/content/c_1') return view
        if (url === '/metadata/content/c_1/overrides') {
            const body = JSON.parse(init!.body as string)
            saves.push(body)
            if (body.rev !== view.overrides_rev) {
                throw new RequestError('Conflict', {
                    response: new Response(null, { status: 409 }),
                    json: { error: 'Changed since it was loaded' },
                })
            }
            return viewAt(view.overrides_rev + 1, body.fields)
        }
        throw new Error(`unexpected ${url}`)
    })
    wrapper = mount(ModalContainer, {
        attachTo: document.body,
        global: {
            plugins: [[VueQueryPlugin, { queryClient: new QueryClient() }]],
            stubs: { ATooltip: { template: '<slot />' } },
        },
    })
})

afterEach(() => {
    wrapper.unmount()
    vi.useRealTimers()
    document.body.innerHTML = ''
})

describe('EditMetadataModal', () => {
    it('saves over the whole overrides layer, keeping edits through a conflict', async () => {
        showEditMetadataModal('c_1')
        await settle()

        button('Clear Title').click()
        await settle()
        // Someone else saves meanwhile.
        view = viewAt(3, { title: 'Theirs', volume: '4' })
        button('Save').click()
        await settle()

        expect(saves[0]).toEqual({ rev: 2, fields: { title: null, volume: '3' } })
        expect(document.body.textContent).toContain('Changed since it was loaded')

        button('Save').click()
        await settle()
        expect(saves[1]).toEqual({ rev: 3, fields: { title: null, volume: '4' } })
    })

    it('resets an override by dropping its key', async () => {
        showEditMetadataModal('c_1')
        await settle()

        button('Reset Title').click()
        await settle()
        expect(document.body.textContent).toContain('Folder')
        button('Save').click()
        await settle()

        expect(saves).toEqual([{ rev: 2, fields: { volume: '3' } }])
    })

    it('reopens and saves unsaved staff edits', async () => {
        showEditMetadataModal('c_1')
        await settle()
        await editStaffName('Ann B')
        expect(document.body.textContent).toContain('Ann B (writer)')

        button('Edit Staff').click()
        await settle()
        expect(field('Name').value).toBe('Ann B')
        button('OK').click()
        await settle()
        button('Save').click()
        await settle()
        expect(saves).toEqual([
            {
                rev: 2,
                fields: { title: 'Mine', volume: '3', staff: [{ name: 'Ann B', role: 'writer' }] },
            },
        ])
    })

    it('restores the local cover by clearing the provider one', async () => {
        showEditMetadataModal('c_1')
        await settle()
        expect(document.body.textContent).toContain(remoteCover.url)

        button('Use the local cover').click()
        await settle()
        expect(document.body.textContent).toContain('Local cover')
        button('Save').click()
        await settle()

        expect(saves).toEqual([{ rev: 2, fields: { title: 'Mine', volume: '3', cover: null } }])
    })
})
