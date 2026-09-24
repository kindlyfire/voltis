import { computed, onMounted, ref, shallowRef, useTemplateRef } from 'vue'
import { normalizeOptions, type Options, type OptionValue } from './options'
import { useOverlayLayer } from './overlay'
import type { FieldProps } from './useFieldIds'

export interface SelectFieldProps extends FieldProps {
    placeholder?: string
    clearable?: boolean
    loading?: boolean
    size?: 'sm' | 'md'
    hintTone?: 'warning'
}

/**
 * State shared by ASelect and ACombobox. Expects a template ref `control` on the Reka trigger or
 * input, inside AField.
 */
export function useSelectField<V extends OptionValue>(
    props: SelectFieldProps & { modelValue: V | null | undefined; options: Options<V> },
    emit: (e: 'update:modelValue', value: V | null) => void
) {
    // Readonly blocks opening, and selection (Reka also selects by typeahead on a closed select).
    const open = ref(false)
    const setOpen = (value: boolean) => props.readonly || (open.value = value)
    // Also makes Esc in an open popup close only the popup, not a drawer or dialog around it.
    useOverlayLayer('menu', open)

    const items = computed(() => normalizeOptions(props.options))

    const fieldProps = computed(() => {
        const { label, hideLabel, hint, hintTone, error, disabled, readonly, id, loading, size } =
            props
        return {
            label,
            hideLabel,
            hint,
            hintTone,
            error,
            disabled,
            readonly,
            id,
            loading,
            size: size ?? 'md',
        }
    })

    const showClear = computed(
        () => props.clearable && !props.disabled && !props.readonly && props.modelValue != null
    )

    const control = useTemplateRef<{ $el: HTMLElement }>('control')
    // The popup lines up with the whole field box.
    const box = shallowRef<HTMLElement>()
    onMounted(
        () => (box.value = control.value?.$el.closest<HTMLElement>('.a-field__box') ?? undefined)
    )

    function select(value: unknown) {
        if (!props.readonly) emit('update:modelValue', (value ?? null) as V | null)
    }

    function clear() {
        emit('update:modelValue', null)
        control.value?.$el.focus()
    }

    return { open, setOpen, items, fieldProps, showClear, box, select, clear, control }
}
