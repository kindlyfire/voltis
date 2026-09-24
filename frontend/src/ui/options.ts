import type { Component } from 'vue'

export type OptionValue = string | number

export interface Option<V extends OptionValue> {
    value: V
    label: string
    disabled?: boolean
    icon?: Component
    /** Secondary text (ARadioGroup). */
    description?: string
}

/** Full options, or bare values that are their own labels. */
export type Options<V extends OptionValue> = readonly Option<V>[] | readonly V[]

export function normalizeOptions<V extends OptionValue>(options: Options<V>): Option<V>[] {
    return options.map(o => (typeof o === 'object' ? o : { value: o, label: String(o) }))
}
