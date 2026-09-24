import { computed, useAttrs, useId } from 'vue'

export type FieldError = string | string[] | undefined

export interface FieldProps {
    label: string
    hideLabel?: boolean
    hint?: string
    error?: FieldError
    disabled?: boolean
    readonly?: boolean
    id?: string
}

/** Ids and ARIA wiring shared by the form controls. */
export function useFieldIds(props: FieldProps) {
    const autoId = useId()
    const id = computed(() => props.id ?? autoId)
    const errors = computed(() => (props.error == null ? [] : [props.error].flat().filter(Boolean)))
    const messageId = computed(() => `${id.value}-message`)
    const hasMessage = computed(() => errors.value.length > 0 || !!props.hint)
    return {
        id,
        errors,
        messageId,
        hasMessage,
        controlAria: computed<ControlAria>(() => ({
            'aria-invalid': errors.value.length ? ('true' as const) : undefined,
            'aria-describedby': hasMessage.value ? messageId.value : undefined,
        })),
    }
}

export type ControlAria = { 'aria-invalid'?: 'true'; 'aria-describedby'?: string }

/**
 * Control attributes with the field's ARIA merged in. A caller's `aria-describedby` is kept next to
 * the message id (and any `extraIds`), and a caller's `aria-invalid` survives when the field is valid.
 */
export function mergeAria(
    attrs: Record<string, unknown>,
    aria: ControlAria,
    ...extraIds: (string | false | undefined)[]
) {
    const describedBy = [attrs['aria-describedby'], ...extraIds, aria['aria-describedby']]
        .filter(Boolean)
        .join(' ')
    return {
        ...attrs,
        'aria-invalid': (aria['aria-invalid'] ?? attrs['aria-invalid']) as 'true' | undefined,
        'aria-describedby': describedBy || undefined,
    }
}

/** Attributes for the actual control of a component with `inheritAttrs: false`: all but class and style,
 * which stay on the root. */
export function useControlAttrs() {
    const attrs = useAttrs()
    return computed(() => {
        const { class: _class, style: _style, ...rest } = attrs
        return rest
    })
}
