<template>
    <AAlert
        v-if="hasError && !dismissed"
        tone="danger"
        :dismissible="closable"
        @dismiss="dismissed = true"
    >
        {{ errorMessage }}
    </AAlert>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { Ref } from 'vue'
import AAlert from '@/ui/AAlert.vue'
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

// A new error shows again after a dismissal.
const dismissed = ref(false)
watch(
    () => source.value?.error.value,
    () => (dismissed.value = false)
)
</script>
