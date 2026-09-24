import { VueQueryPlugin } from '@tanstack/vue-query'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { z } from 'zod'
import { useForm } from './forms'

function setup() {
    let form!: ReturnType<typeof make>
    const make = () =>
        useForm({
            schema: z.object({ name: z.string().min(3, 'Too short'), days: z.number() }),
            initialValues: { name: '', days: 30 },
        })
    mount(
        defineComponent({
            setup() {
                form = make()
                return () => null
            },
        }),
        { global: { plugins: [VueQueryPlugin] } }
    )
    return form
}

describe('useForm().field', () => {
    it('reads and writes the value at its path', () => {
        const form = setup()
        expect(form.field('days').modelValue).toBe(30)
        form.field('name')['onUpdate:modelValue']('Ann')
        expect(form.values.value.name).toBe('Ann')
    })

    it('validates on blur only once the field was touched', () => {
        const form = setup()
        form.field('name').onBlur()
        expect(form.field('name').error).toBeUndefined()

        form.field('name')['onUpdate:modelValue']('A')
        form.field('name').onBlur()
        expect(form.field('name').error).toEqual(['Too short'])
    })

    it('revalidates an invalid field on update, and maps errors to strings', () => {
        const form = setup()
        form.onSubmit()
        expect(form.field('name').error).toEqual(['Too short'])
        expect(form.field('days').error).toBeUndefined()

        form.field('name')['onUpdate:modelValue']('Anna')
        expect(form.field('name').error).toBeUndefined()
    })

    it('focuses the first invalid control of the submitted form', async () => {
        const form = setup()
        const el = document.createElement('form')
        el.innerHTML = '<input id="ok"><input id="bad" aria-invalid="true">'
        document.body.append(el)
        form.onSubmit({ target: el, preventDefault() {} } as unknown as Event)
        await nextTick()
        expect(document.activeElement?.id).toBe('bad')
        el.remove()
    })
})
