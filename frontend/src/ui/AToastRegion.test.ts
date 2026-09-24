import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { h, nextTick } from 'vue'
import AToastRegion from './AToastRegion.vue'
import { toasts, useToast } from './useToast'

beforeEach(() => {
    vi.useFakeTimers()
    toasts.value = []
})
afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
})

async function settle(ms = 0) {
    await vi.advanceTimersByTimeAsync(ms)
    await nextTick()
}

it('keeps the other toasts expiring after one is dismissed with the keyboard', async () => {
    const wrapper = mount(
        { render: () => [h('button', { id: 'outside' }, 'Outside'), h(AToastRegion)] },
        { attachTo: document.body }
    )
    const { show } = useToast()
    show({ message: 'first' })
    show({ message: 'second' })
    await settle()

    // Focus in the region pauses both, past their duration.
    const [first] = document.querySelectorAll<HTMLElement>('.a-toast')
    first!.focus()
    await settle(10_000)
    expect(document.querySelectorAll('.a-toast')).toHaveLength(2)

    // A keyboard click has an empty pointerType.
    const dismiss = first!.querySelector<HTMLElement>('[aria-label="Dismiss"]')!
    dismiss.focus()
    dismiss.dispatchEvent(new PointerEvent('click', { bubbles: true, pointerType: '' }))
    await settle(500)
    expect(toasts.value.map(t => t.message)).toEqual(['second'])

    document.getElementById('outside')!.focus()
    await settle(4500)
    expect(toasts.value).toEqual([])
    wrapper.unmount()
})

it('closes only the focused toast on Escape, and none on an Escape elsewhere', async () => {
    const wrapper = mount(
        { render: () => [h('button', { id: 'outside' }, 'Outside'), h(AToastRegion)] },
        { attachTo: document.body }
    )
    const { show } = useToast()
    show({ message: 'first' })
    show({ message: 'second' })
    await settle()
    const escape = (el: Element) =>
        el.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))

    escape(document.getElementById('outside')!)
    await settle(300)
    expect(toasts.value.map(t => t.message)).toEqual(['first', 'second'])

    const second = document.querySelectorAll<HTMLElement>('.a-toast')[1]!
    second.focus()
    escape(second)
    await settle(300)
    expect(toasts.value.map(t => t.message)).toEqual(['first'])
    wrapper.unmount()
})

it('F8 focuses the newest toast, and Esc then closes it', async () => {
    const wrapper = mount(
        { render: () => [h('button', { id: 'outside' }, 'Outside'), h(AToastRegion)] },
        { attachTo: document.body }
    )
    const { show } = useToast()
    show({ message: 'first' })
    show({ message: 'second' })
    await settle()
    const outside = document.getElementById('outside')!
    outside.focus()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'F8', code: 'F8', bubbles: true }))
    await settle()
    const active = document.activeElement!
    expect(active.textContent).toContain('second')

    active.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await settle(300)
    expect(toasts.value.map(t => t.message)).toEqual(['first'])
    wrapper.unmount()
})
