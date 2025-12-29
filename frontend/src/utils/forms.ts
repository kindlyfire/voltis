import { useMutation } from '@tanstack/vue-query'
import { z } from 'zod'
import { getByPath, setByPath, type Path, type PathValue } from './dot-path-value'
import { computed, reactive, toRef } from 'vue'

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

    function getInputProps<T extends Path<z.input<TSchema>>>(name: T) {
        const errors = computed(() => state.errors.filter(error => error.path.join('.') === name))
        const isTouched = computed(() => state.touched.has(name))
        const isDirty = computed(() => {
            const current = getByPath(state.values as any, name)
            const initial = getByPath(initialValues as any, name)
            return !isEqual(current, initial)
        })

        return reactive({
            modelValue: computed(() => getByPath(state.values as any, name)),
            'onUpdate:modelValue': (value: any) => {
                state.touched.add(name)
                setByPath(state.values as any, name, value)
                if (errors.value.length) validatePath(name)
            },
            onBlur: () => {
                if (state.touched.has(name)) {
                    validatePath(name)
                }
            },
            errors,
            isTouched,
            isDirty,
        })
    }

    function onSubmit(e?: Event) {
        if (e) {
            e.preventDefault()
        }

        const result = options.schema.safeParse(state.values)
        if (!result.success) {
            console.log('Form errors:', result.error.issues)
            state.errors = result.error.issues
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
        getInputProps,
        onSubmit,
        reset,
        resetField,
        mutation,
    }
}
