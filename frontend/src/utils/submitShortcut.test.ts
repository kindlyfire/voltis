import { afterEach, describe, expect, it, vi } from 'vitest'
import { onSubmitShortcut } from './submitShortcut'

afterEach(() => {
    document.body.innerHTML = ''
})

function setup(html: string) {
    document.body.innerHTML = html
    const onSubmit = vi.fn((e: Event) => e.preventDefault())
    document.querySelector('form')?.addEventListener('submit', onSubmit)
    document.addEventListener('keydown', onSubmitShortcut)
    const press = (init: KeyboardEventInit = { ctrlKey: true }) => {
        const e = new KeyboardEvent('keydown', {
            key: 'Enter',
            bubbles: true,
            cancelable: true,
            ...init,
        })
        document.querySelector('textarea')!.dispatchEvent(e)
        document.removeEventListener('keydown', onSubmitShortcut)
        return e
    }
    return { onSubmit, press }
}

describe('onSubmitShortcut', () => {
    it('submits the form with its submit button, also one linked with form=', () => {
        const { onSubmit, press } = setup(
            '<form id="f"><textarea></textarea></form><button type="submit" form="f">Save</button>'
        )
        expect(press().defaultPrevented).toBe(true)
        expect(onSubmit).toHaveBeenCalledTimes(1)
        expect((onSubmit.mock.calls[0]![0] as SubmitEvent).submitter?.textContent).toBe('Save')
    })

    it('needs the modifier', () => {
        const { onSubmit, press } = setup('<form><textarea></textarea><button>Save</button></form>')
        press({})
        expect(onSubmit).not.toHaveBeenCalled()
    })

    it('ignores an Enter that ends a composition', () => {
        for (const init of [{ isComposing: true }, { keyCode: 229 }]) {
            const { onSubmit, press } = setup(
                '<form><textarea></textarea><button type="submit">Save</button></form>'
            )
            press({ ctrlKey: true, ...init })
            expect(onSubmit).not.toHaveBeenCalled()
        }
    })

    it('does nothing while the submit button is disabled or loading', () => {
        for (const attrs of ['disabled', 'aria-busy="true"']) {
            const { onSubmit, press } = setup(
                `<form><textarea></textarea><button type="submit" ${attrs}>Save</button></form>`
            )
            press({ metaKey: true })
            expect(onSubmit).not.toHaveBeenCalled()
        }
    })

    it('does nothing outside a form', () => {
        const { press } = setup('<div role="dialog"><textarea></textarea><button>OK</button></div>')
        expect(press().defaultPrevented).toBe(false)
    })
})
