<template>
    <VAlert v-if="hasError" type="error" variant="tonal" class="text-sm!" :closable="closable">
        {{ errorMessage }}
    </VAlert>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Ref } from 'vue'
import { RequestError } from '@/utils/fetch'

type ErrorSource = {
    isError: Ref<boolean>
    error: Ref<unknown>
}

const props = withDefaults(
    defineProps<{
        mutation?: ErrorSource
        query?: ErrorSource
        closable?: boolean
    }>(),
    {
        closable: false,
    }
)

const source = computed(() => props.mutation ?? props.query)

const hasError = computed(() => {
    return source.value?.isError.value ?? false
})

const errorMessage = computed(() => {
    const error = source.value?.error.value
    if (!error) return ''
    return RequestError.getMessage(error)
})
</script>
