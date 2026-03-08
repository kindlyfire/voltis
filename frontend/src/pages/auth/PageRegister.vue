<template>
    <VContainer>
        <VRow justify="center">
            <VCol cols="12" sm="8" md="4">
                <VCard>
                    <VCardTitle class="text-h5">Register</VCardTitle>
                    <VCardText>
                        <div
                            v-if="infoQuery.isLoading.value"
                            class="my-4 flex items-center justify-center"
                        >
                            <VProgressCircular indeterminate size="64" />
                        </div>
                        <div v-else-if="registrationsEnabled">
                            <VAlert
                                v-if="isFirstUserFlow"
                                type="info"
                                variant="tonal"
                                class="mt-2 mb-4"
                            >
                                Welcome! Create the first admin account below to get started.
                            </VAlert>
                            <VForm @submit="onSubmit" class="space-y-4!">
                                <AInput
                                    :input="getInputProps('username')"
                                    label="Username"
                                    autofocus
                                />
                                <AInput
                                    :input="getInputProps('password')"
                                    label="Password"
                                    type="password"
                                />
                                <AInput
                                    :input="getInputProps('confirmPassword')"
                                    label="Confirm Password"
                                    type="password"
                                />
                                <AQueryError :mutation="mutation" />
                                <VBtn
                                    type="submit"
                                    color="primary"
                                    block
                                    :loading="mutation.isPending.value"
                                    class="mt-4"
                                >
                                    Register
                                </VBtn>
                            </VForm>
                        </div>
                        <div v-else class="text-body-2">
                            Registrations are currently disabled. Please contact an administrator
                            for access.
                        </div>
                    </VCardText>
                    <VCardActions v-if="!isFirstUserFlow">
                        <VSpacer />
                        <RouterLink to="/auth/login">Already have an account?</RouterLink>
                    </VCardActions>
                </VCard>
            </VCol>
        </VRow>
    </VContainer>
</template>

<script setup lang="ts">
import { useQueryClient } from '@tanstack/vue-query'
import { useHead } from '@unhead/vue'
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { z } from 'zod'
import AInput from '@/components/AInput.vue'
import AQueryError from '@/components/AQueryError.vue'
import { authApi } from '@/utils/api/auth'
import { miscApi } from '@/utils/api/misc'
import { useForm } from '@/utils/forms'
import { useAlreadyLoggedInRedirect } from './PageLogin.vue'

useHead({
    title: 'Register',
})

const register = authApi.useRegister()
const router = useRouter()
const queryClient = useQueryClient()
useAlreadyLoggedInRedirect()

const infoQuery = miscApi.useInfo()
const isFirstUserFlow = computed(() => infoQuery.data.value?.first_user_flow ?? false)
const registrationsEnabled = computed(
    () => isFirstUserFlow.value || (infoQuery.data.value?.registration_enabled ?? false)
)

const schema = z
    .object({
        username: z.string().min(3),
        password: z.string().min(8),
        confirmPassword: z.string(),
    })
    .refine(data => data.password === data.confirmPassword, {
        message: 'Passwords do not match',
        path: ['confirmPassword'],
    })

const { getInputProps, onSubmit, mutation } = useForm({
    schema,
    initialValues: {
        username: '',
        password: '',
        confirmPassword: '',
    },
    onSubmit: async values => {
        await register.mutateAsync({
            username: values.username,
            password: values.password,
        })
        await Promise.all([
            queryClient.refetchQueries({
                queryKey: ['misc', 'info'],
            }),
            queryClient.refetchQueries({
                queryKey: ['users', 'me'],
            }),
        ])
        router.push('/')
    },
})
</script>
