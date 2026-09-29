<template>
    <AuthCard title="Register" :loading="infoQuery.isLoading.value">
        <div v-if="registrationsEnabled" class="flex flex-col gap-4 pt-2">
            <AAlert v-if="isFirstUserFlow" tone="info">
                Welcome! Create the first admin account below to get started.
            </AAlert>
            <form novalidate class="flex flex-col gap-4" @submit="onSubmit">
                <ATextField
                    v-bind="field('username')"
                    label="Username"
                    autocomplete="username"
                    autofocus
                />
                <ATextField
                    v-bind="field('password')"
                    label="Password"
                    type="password"
                    autocomplete="new-password"
                />
                <ATextField
                    v-bind="field('confirmPassword')"
                    label="Confirm Password"
                    type="password"
                    autocomplete="new-password"
                />
                <QueryError :mutation="mutation" />
                <AButton type="submit" block size="lg" :loading="mutation.isPending.value">
                    Register
                </AButton>
            </form>
        </div>
        <p v-else class="text-fg-muted text-sm leading-normal">
            Registrations are currently disabled. Please contact an administrator for access.
        </p>
        <template v-if="!isFirstUserFlow" #footer>
            <AButton
                variant="text"
                :to="{ path: '/auth/login', query: redirectQuery(route.query.redirect) }"
            >
                Already have an account?
            </AButton>
        </template>
    </AuthCard>
</template>

<script setup lang="ts">
import { useQueryClient } from '@tanstack/vue-query'
import { useHead } from '@unhead/vue'
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { z } from 'zod'
import QueryError from '@/components/QueryError.vue'
import AAlert from '@/ui/AAlert.vue'
import AButton from '@/ui/AButton.vue'
import ATextField from '@/ui/ATextField.vue'
import { authApi } from '@/utils/api/auth'
import { miscApi } from '@/utils/api/misc'
import { useForm } from '@/utils/forms'
import { redirectQuery } from '@/utils/redirect'
import AuthCard from './AuthCard.vue'
import { useAlreadyLoggedInRedirect } from './PageLogin.vue'

useHead({
    title: 'Register',
})

const register = authApi.useRegister()
const route = useRoute()
const queryClient = useQueryClient()
useAlreadyLoggedInRedirect()

const infoQuery = miscApi.useInfo()
const isFirstUserFlow = computed(() => infoQuery.data.value?.first_user_flow ?? false)
const registrationsEnabled = computed(
    () => isFirstUserFlow.value || (infoQuery.data.value?.registration_enabled ?? false)
)

const schema = z
    .object({
        username: z.string().min(3, 'Use at least 3 characters'),
        password: z.string().min(8, 'Use at least 8 characters'),
        confirmPassword: z.string(),
    })
    .refine(data => data.password === data.confirmPassword, {
        message: 'Passwords do not match',
        path: ['confirmPassword'],
    })

const { field, onSubmit, mutation } = useForm({
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
        // `useAlreadyLoggedInRedirect` navigates once `me` is refetched.
        await Promise.all([
            queryClient.refetchQueries({
                queryKey: ['misc', 'info'],
            }),
            queryClient.refetchQueries({
                queryKey: ['users', 'me'],
            }),
        ])
    },
})
</script>
