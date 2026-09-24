<template>
    <VContainer>
        <VRow justify="center">
            <VCol cols="12" sm="8" md="4">
                <VCard>
                    <VCardTitle class="text-h5">Login</VCardTitle>
                    <VCardText>
                        <VAlert v-if="error" type="error" variant="tonal" class="mb-4">
                            {{ error }}
                        </VAlert>

                        <VBtn
                            v-if="info?.oidc_enabled"
                            block
                            color="primary"
                            variant="tonal"
                            prepend-icon="mdi-shield-account"
                            @click="startSso"
                        >
                            {{ info.oidc_button_label || 'Sign in with SSO' }}
                        </VBtn>

                        <div
                            v-if="info?.oidc_enabled && passwordLogin"
                            class="my-4 flex items-center gap-3"
                        >
                            <VDivider class="grow" />
                            <span class="text-xs opacity-60">or</span>
                            <VDivider class="grow" />
                        </div>

                        <VForm v-if="passwordLogin" @submit="onSubmit" class="space-y-4!">
                            <AInput :input="getInputProps('username')" label="Username" autofocus />
                            <AInput
                                :input="getInputProps('password')"
                                label="Password"
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
                                Login
                            </VBtn>
                        </VForm>

                        <VAlert v-else-if="info && !info.oidc_enabled" type="info" variant="tonal">
                            No sign-in method is enabled. Ask an administrator.
                        </VAlert>
                    </VCardText>
                    <VCardActions v-if="passwordLogin && info?.registration_enabled">
                        <VSpacer />
                        <RouterLink to="/auth/register">Don't have an account?</RouterLink>
                    </VCardActions>
                </VCard>
            </VCol>
        </VRow>
    </VContainer>
</template>

<script setup lang="ts">
import { useQueryClient } from '@tanstack/vue-query'
import { useHead } from '@unhead/vue'
import { computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { z } from 'zod'
import AInput from '@/components/AInput.vue'
import AQueryError from '@/components/AQueryError.vue'
import { authApi } from '@/utils/api/auth'
import { miscApi } from '@/utils/api/misc'
import { clearSignedOut, isSignedOut, OIDC_LOGIN_URL, shouldAutoRedirect } from '@/utils/api/oidc'
import { usersApi } from '@/utils/api/users'
import { useForm } from '@/utils/forms'

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
    username: z.string().min(1),
    password: z.string().min(1),
})

const { getInputProps, onSubmit, mutation } = useForm({
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
