import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { hasOpenModal, ModalContainer, Modals } from './modals'
import { addOverlays, settle, TestDialog as Dialog } from './modalTesting'

function opener() {
    const button = document.createElement('button')
    document.body.append(button)
    button.focus()
    return button
}

function render() {
    const wrapper = mount(ModalContainer, { attachTo: document.body })
    const closeLast = () => wrapper.findAllComponents(Dialog).at(-1)!.props('close')
    return { wrapper, closeLast }
}

beforeEach(() => {
    vi.useFakeTimers()
    addOverlays()
})

afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
})

describe('Modals', () => {
    it('resolves once when close is called repeatedly, and removes the entry after leaving', async () => {
        const { wrapper, closeLast } = render()
        const result = vi.fn()
        Modals.show<string>(Dialog).then(result)
        await settle()

        const close = closeLast()
        close('first')
        close('second')
        await settle()
        expect(result).toHaveBeenCalledTimes(1)
        expect(result).toHaveBeenCalledWith('first')
        expect(hasOpenModal.value).toBe(false)
        expect(wrapper.findAllComponents(Dialog)).toHaveLength(0)
        wrapper.unmount()
    })

    it('rejects without resolving when its signal aborts, at any point', async () => {
        const { wrapper, closeLast } = render()
        const resolved = vi.fn()
        const reason = new Error('left')

        const open = new AbortController()
        const rejected = expect(
            Modals.show(Dialog, {}, { signal: open.signal }).then(resolved)
        ).rejects.toBe(reason)
        await settle()
        const close = closeLast()
        open.abort(reason)
        await rejected
        await settle()
        close('late')
        expect(resolved).not.toHaveBeenCalled()
        expect(hasOpenModal.value).toBe(false)
        expect(wrapper.findAllComponents(Dialog)).toHaveLength(0)

        // Before the dialog content mounted there is no leave transition to remove the entry.
        const early = new AbortController()
        const earlyRejected = expect(
            Modals.show(Dialog, {}, { signal: early.signal })
        ).rejects.toBe(reason)
        await nextTick()
        early.abort(reason)
        await earlyRejected
        await settle()
        expect(wrapper.findAllComponents(Dialog)).toHaveLength(0)

        await expect(Modals.show(Dialog, {}, { signal: AbortSignal.abort(reason) })).rejects.toBe(
            reason
        )
        await settle()
        expect(wrapper.findAllComponents(Dialog)).toHaveLength(0)
        wrapper.unmount()
    })

    it('returns focus to the opener, or to the fallback once it is gone', async () => {
        const { wrapper, closeLast } = render()
        const first = opener()
        Modals.show(Dialog)
        await settle()
        expect(document.activeElement?.getAttribute('role')).toBe('dialog')
        closeLast()()
        await settle()
        expect(document.activeElement).toBe(first)

        const gone = opener()
        const fallback = opener()
        gone.focus()
        Modals.show(Dialog, {}, { focusFallback: () => fallback })
        await settle()
        gone.remove()
        closeLast()()
        await settle()
        expect(document.activeElement).toBe(fallback)
        wrapper.unmount()
    })

    it('resolves a menu item opener to the menu trigger', async () => {
        const { wrapper, closeLast } = render()
        const trigger = opener()
        trigger.id = 'trigger'
        const menu = document.createElement('div')
        menu.setAttribute('role', 'menu')
        menu.setAttribute('aria-labelledby', 'trigger')
        const item = document.createElement('div')
        item.tabIndex = -1
        menu.append(item)
        document.body.append(menu)
        item.focus()

        Modals.show(Dialog)
        await settle()
        menu.remove()
        closeLast()()
        await settle()
        expect(document.activeElement).toBe(trigger)
        wrapper.unmount()
    })
})
