import { useEventListener } from '@vueuse/core'

/**
 * Ctrl+Enter (Cmd+Enter on macOS) submits the form that owns the focused element, as a click on
 * its submit button would (validation, loading state). Plain Enter can't in a multiline field.
 * Outside a form it does nothing: it must not confirm a dialog's destructive action.
 */
export function onSubmitShortcut(e: KeyboardEvent) {
    if (!(e.ctrlKey || e.metaKey) || e.key !== 'Enter' || e.defaultPrevented) return
    // Safari ends a composition with an Enter keydown that has keyCode 229 but not isComposing.
    if (e.isComposing || e.keyCode === 229) return
    const target = e.target
    if (!(target instanceof Element)) return
    const form =
        ('form' in target && target.form instanceof HTMLFormElement ? target.form : null) ??
        target.closest('form')
    if (!form) return
    // `elements` includes buttons outside the form linked with `form=` (dialog actions).
    const button = [...form.elements].find(
        (el): el is HTMLButtonElement | HTMLInputElement =>
            (el instanceof HTMLButtonElement || el instanceof HTMLInputElement) &&
            el.type === 'submit'
    )
    if (
        !button ||
        button.disabled ||
        button.getAttribute('aria-disabled') === 'true' ||
        button.getAttribute('aria-busy') === 'true'
    ) {
        return
    }
    e.preventDefault()
    form.requestSubmit(button)
}

/** Installs `onSubmitShortcut` for the whole app. Call once, in the app root. */
export function useSubmitShortcut() {
    useEventListener(document, 'keydown', onSubmitShortcut)
}
