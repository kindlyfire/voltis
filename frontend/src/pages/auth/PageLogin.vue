<template>
    <AuthCard title="Login">
        <div class="flex flex-col gap-4 pt-2">
            <AAlert v-if="error" tone="danger">{{ error }}</AAlert>

            <AButton
                v-if="info?.oidc_enabled"
                block
                variant="tonal"
                :leading-icon="IconShieldAccount"
                @click="startSso"
            >
                {{ info.oidc_button_label || 'Sign in with SSO' }}
            </AButton>

            <div
                v-if="info?.oidc_enabled && passwordLogin"
                class="text-fg-muted flex items-center gap-3 text-xs"
            >
                <ADivider class="grow" />
                or
                <ADivider class="grow" />
            </div>

            <form v-if="passwordLogin" novalidate class="flex flex-col gap-4" @submit="onSubmit">
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
                    autocomplete="current-password"
                />
                <QueryError :mutation="mutation" />
                <AButton type="submit" block size="lg" :loading="mutation.isPending.value">
                    Login
                </AButton>
            </form>

            <AAlert v-else-if="info && !info.oidc_enabled" tone="info">
                No sign-in method is enabled. Ask an administrator.
            </AAlert>
        </div>
        <template v-if="passwordLogin && info?.registration_enabled" #footer>
            <AButton variant="text" to="/auth/register">Don't have an account?</AButton>
        </template>
    </AuthCard>
</template>

<script setup lang="ts">
import { useQueryClient } from '@tanstack/vue-query'
import { useHead } from '@unhead/vue'
import { computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { z } from 'zod'
import QueryError from '@/components/QueryError.vue'
import AAlert from '@/ui/AAlert.vue'
import AButton from '@/ui/AButton.vue'
import ADivider from '@/ui/ADivider.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconShieldAccount } from '@/ui/icons'
import { authApi } from '@/utils/api/auth'
import { miscApi } from '@/utils/api/misc'
import { clearSignedOut, isSignedOut, OIDC_LOGIN_URL, shouldAutoRedirect } from '@/utils/api/oidc'
import { usersApi } from '@/utils/api/users'
import { useForm } from '@/utils/forms'
import AuthCard from './AuthCard.vue'

useHead({
    title: 'Login',
})

const login = authApi.useLogin()
const router = useRouter()
const route = useRoute()
const queryClient = useQueryClient()
const qInfo = miscApi.useInfo()
useAlreadyLoggedInRedirect()

const info = computed(() => qInfo.data.value)
const error = computed(() => (route.query.error as string) || '')
const hasError = computed(() => 'error' in route.query)
const passwordLogin = computed(() => info.value?.password_login_enabled !== false)

function startSso() {
    clearSignedOut()
    window.location.href = OIDC_LOGIN_URL
}

watch(
    () => [info.value, hasError.value, route.query.local] as const,
    ([i, failed, local]) => {
        if (shouldAutoRedirect(i, failed, local, isSignedOut())) {
            startSso()
        }
    },
    { immediate: true }
)

const schema = z.object({
    username: z.string().min(1, 'Enter your username'),
    password: z.string().min(1, 'Enter your password'),
})

const { field, onSubmit, mutation } = useForm({
    schema,
    initialValues: {
        username: '',
        password: '',
    },
    onSubmit: async values => {
        await login.mutateAsync({
            username: values.username,
            password: values.password,
        })
        await queryClient.refetchQueries({
            queryKey: ['users', 'me'],
        })
        router.push('/')
    },
})
</script>

<script lang="ts">
export function useAlreadyLoggedInRedirect() {
    const router = useRouter()
    const qMe = usersApi.useMe()
    watch(
        () => qMe.data.value,
        me => {
            if (me) {
                router.push('/')
            }
        },
        { immediate: true }
    )
}
</script>
