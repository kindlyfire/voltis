import { useMutation } from '@tanstack/vue-query'
import { computed, nextTick, reactive, toRef } from 'vue'
import { z } from 'zod'
import { getByPath, setByPath, type Path, type PathValue } from './dot-path-value'

interface UseFormOptions<TSchema extends z.ZodTypeAny, TMutationReturn> {
    schema: TSchema
    validateAsync?: (values: z.output<TSchema>, ctx: z.RefinementCtx) => Promise<any>
    initialValues: z.input<TSchema>
    onSubmit?: (values: z.output<TSchema>) => TMutationReturn
    clone?: (values: z.input<TSchema>) => z.input<TSchema>
}

function defaultClone<T>(value: T): T {
    return JSON.parse(JSON.stringify(value))
}

function isEqual(a: unknown, b: unknown): boolean {
    return JSON.stringify(a) === JSON.stringify(b)
}

export function useForm<TSchema extends z.ZodTypeAny, TMutationReturn>(
    options: UseFormOptions<TSchema, TMutationReturn>
) {
    const clone = options.clone ?? defaultClone
    const initialValues = clone(options.initialValues)

    const state = reactive({
        values: clone(options.initialValues),
        errors: [] as z.ZodIssue[],
        touched: new Set<string>(),
    }) as {
        values: z.input<TSchema>
        errors: z.ZodIssue[]
        touched: Set<string>
    }

    function validatePath(path: string) {
        const result = options.schema.safeParse(state.values)
        const newErrors = result.error?.issues.filter(error => error.path.join('.') === path)
        state.errors = state.errors
            .filter(error => error.path.join('.') !== path)
            .concat(newErrors ?? [])
    }

    function errorsFor(path: string): string[] | undefined {
        const messages = state.errors.filter(e => e.path.join('.') === path).map(e => e.message)
        return messages.length ? messages : undefined
    }

    /**
     * Props for a kit form control: `<ATextField v-bind="form.field('email')" label="Email" />`.
     * An update marks the field touched and revalidates it if it has errors; blur validates
     * touched fields. Call it in the template, so it reads the current state on each render.
     */
    function field<T extends Path<z.input<TSchema>>>(name: T) {
        return {
            modelValue: getByPath(state.values as any, name) as PathValue<z.input<TSchema>, T>,
            'onUpdate:modelValue': (value: PathValue<z.input<TSchema>, T>) => {
                state.touched.add(name)
                setByPath(state.values as any, name, value as any)
                if (errorsFor(name)) validatePath(name)
            },
            onBlur: () => {
                if (state.touched.has(name)) validatePath(name)
            },
            error: errorsFor(name),
        }
    }

    function onSubmit(e?: Event) {
        if (e) {
            e.preventDefault()
        }

        const result = options.schema.safeParse(state.values)
        if (!result.success) {
            console.log('Form errors:', result.error.issues)
            state.errors = result.error.issues
            // Take the user to the first problem.
            const form = e?.target instanceof HTMLFormElement ? e.target : null
            if (form) {
                void nextTick(() =>
                    form.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
                )
            }
            return
        }
        mutation.mutate(result.data)
    }

    function setValues(values: z.input<TSchema>) {
        state.values = values
    }

    function setValue<T extends Path<z.input<TSchema>>>(
        name: T,
        value: PathValue<z.input<TSchema>, T>
    ) {
        setByPath(state.values as any, name, value as any)
    }

    function reset(newInitialValues?: z.input<TSchema>) {
        const resetTo = newInitialValues ?? initialValues
        state.values = clone(resetTo)
        state.errors = []
        state.touched.clear()
    }

    function resetField<T extends Path<z.input<TSchema>>>(name: T) {
        const initialValue = getByPath(initialValues as any, name)
        setByPath(state.values as any, name, initialValue as any)
        state.errors = state.errors.filter(error => error.path.join('.') !== name)
        state.touched.delete(name)
    }

    const isValid = computed(() => state.errors.length === 0)

    const isDirty = computed(() => !isEqual(state.values, initialValues))

    const mutation = useMutation({
        async mutationFn(values: z.output<TSchema>) {
            if (options.validateAsync) {
                const result = await options.schema
                    .superRefine(async (data, ctx) => {
                        return options.validateAsync!(data, ctx)
                    })
                    .safeParseAsync(state.values)
                if (result.error) {
                    console.log('Form errors:', result.error.issues)
                    state.errors = result.error.issues
                    return
                }
            }
            return await options.onSubmit?.(values)
        },
    })

    return {
        values: toRef(state, 'values'),
        errors: toRef(state, 'errors'),
        touched: computed(() => state.touched),
        isValid,
        isDirty,
        setValues,
        setValue,
        field,
        onSubmit,
        reset,
        resetField,
        mutation,
    }
}
