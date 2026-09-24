<template>
    <VContainer>
        <h1 class="mb-6 text-4xl">Account</h1>

        <VCard class="mb-6">
            <VCardTitle>User Details</VCardTitle>
            <VCardText>
                <VForm @submit="detailsForm.onSubmit" class="space-y-4!">
                    <AInput :input="detailsForm.getInputProps('username')" label="Username" />
                    <AInput
                        :input="detailsForm.getInputProps('email')"
                        label="Email"
                        type="email"
                    />
                    <AQueryError :mutation="detailsForm.mutation" />
                    <VBtn
                        type="submit"
                        color="primary"
                        :loading="detailsForm.mutation.isPending.value"
                        class="mt-4"
                    >
                        Save
                    </VBtn>
                </VForm>
            </VCardText>
        </VCard>

        <IdentitiesCard
            v-if="me.data.value"
            class="mb-6"
            :user-id="'me'"
            self
            @unlinked="signedOut"
        >
            <VAlert v-if="linkError" type="error" variant="tonal" class="mb-4">
                {{ linkError }}
            </VAlert>
            <VBtn
                v-if="info.data.value?.oidc_enabled"
                variant="tonal"
                prepend-icon="mdi-link-variant"
                :loading="link.isPending.value"
                @click="connectSso"
            >
                Connect SSO
            </VBtn>
            <AQueryError :mutation="link" />
        </IdentitiesCard>

        <VCard v-if="info.data.value?.password_login_enabled">
            <VCardTitle>{{
                me.data.value?.has_password ? 'Change Password' : 'Set Password'
            }}</VCardTitle>
            <VCardText>
                <VForm @submit="passwordForm.onSubmit">
                    <AInput
                        :input="passwordForm.getInputProps('password')"
                        label="New Password"
                        type="password"
                    />
                    <AQueryError :mutation="passwordForm.mutation" />
                    <VBtn
                        type="submit"
                        color="primary"
                        :loading="passwordForm.mutation.isPending.value"
                        class="mt-4"
                    >
                        Update Password
                    </VBtn>
                </VForm>
            </VCardText>
        </VCard>
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
import { miscApi } from '@/utils/api/misc'
import { oidcApi } from '@/utils/api/oidc'
import { usersApi } from '@/utils/api/users'
import { useForm } from '@/utils/forms'
import IdentitiesCard from './IdentitiesCard.vue'

useHead({
    title: 'Account',
})

const me = usersApi.useMe()
const info = miscApi.useInfo()
const upsert = usersApi.useUpdateMe()
const link = oidcApi.useLink()
const queryClient = useQueryClient()
const route = useRoute()
const router = useRouter()

// The SSO callback reports link failures by sending the browser back here.
const linkError = computed(() => (route.query.error as string) || '')

async function connectSso() {
    const { url } = await link.mutateAsync()
    window.location.href = url
}

// Every session is gone. local=1 stops auto-redirect signing them back in.
function signedOut() {
    queryClient.invalidateQueries({ queryKey: ['users', 'me'] })
    router.push('/auth/login?local=1')
}

const detailsForm = useForm({
    schema: z.object({
        username: z.string().min(3),
        email: z.string(),
    }),
    initialValues: {
        username: '',
        email: '',
    },
    onSubmit: async values => {
        await upsert.mutateAsync({
            username: values.username,
            email: values.email,
        })
    },
})

watch(
    () => me.data.value,
    user => {
        if (user) {
            detailsForm.setValues({
                username: user.username,
                email: user.email ?? '',
            })
        }
    },
    { immediate: true }
)

const passwordForm = useForm({
    schema: z.object({
        password: z.string().min(8),
    }),
    initialValues: {
        password: '',
    },
    onSubmit: async values => {
        const _me = me.data.value
        if (!_me) return

        await upsert.mutateAsync({
            username: _me.username,
            password: values.password,
        })
        passwordForm.reset()
    },
})
</script>
