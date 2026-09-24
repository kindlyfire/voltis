import { computed } from 'vue'
import { RouterLink, type RouteLocationRaw } from 'vue-router'

export interface ButtonRootProps {
    type?: 'button' | 'submit' | 'reset'
    to?: RouteLocationRaw
    href?: string
    disabled?: boolean
    /** Disabled with `aria-disabled` instead, so the button keeps focus when it disables itself
     * (a "next" button reaching the end, a press-and-hold limit). Only `click` is blocked:
     * pointerdown/keydown handlers must check `aria-disabled` themselves. */
    focusableWhenDisabled?: boolean
    loading?: boolean
}

/**
 * The root element shared by AButton and AIconButton: a RouterLink for `to`, an external link
 * for `href`, otherwise a native button. Disabled links drop their target and use `aria-disabled`.
 */
export function useButtonRoot(props: ButtonRootProps) {
    const root = computed(() => {
        const link = props.to != null || props.href != null
        if (link && props.disabled) {
            return { is: 'a', attrs: { 'aria-disabled': 'true', role: 'link' } }
        }
        if (props.to != null) return { is: RouterLink, attrs: { to: props.to } }
        if (props.href != null) {
            return { is: 'a', attrs: { href: props.href, target: '_blank', rel: 'noopener' } }
        }
        const inert = props.disabled && props.focusableWhenDisabled
        return {
            is: 'button',
            attrs: {
                type: props.type ?? 'button',
                disabled: props.disabled && !inert,
                'aria-disabled': inert ? 'true' : undefined,
            },
        }
    })

    /** Loading keeps the button focusable and named, but blocks click, Enter and Space. */
    function onClickCapture(e: MouseEvent) {
        if (props.loading || props.disabled) {
            e.preventDefault()
            e.stopImmediatePropagation()
        }
    }

    return {
        root,
        rootAttrs: computed(() => ({
            ...root.value.attrs,
            'aria-busy': props.loading ? 'true' : undefined,
            onClickCapture,
        })),
    }
}
