<template>
    <div class="settings-page max-w-[780px]">
        <APageHeader title="Account" class="mb-1.5" />

        <ACard title="User details">
            <form
                :id="detailsFormId"
                novalidate
                class="flex flex-col gap-3"
                @submit="detailsForm.onSubmit"
            >
                <ATextField
                    v-bind="detailsForm.field('username')"
                    label="Username"
                    autocomplete="username"
                />
                <ATextField
                    v-bind="detailsForm.field('email')"
                    label="Email"
                    type="email"
                    autocomplete="email"
                />
                <QueryError :mutation="detailsForm.mutation" />
            </form>
            <template #actions>
                <AButton
                    type="submit"
                    :form="detailsFormId"
                    :loading="detailsForm.mutation.isPending.value"
                >
                    Save
                </AButton>
            </template>
        </ACard>

        <IdentitiesCard v-if="me.data.value" user-id="me" self @unlinked="signedOut">
            <AAlert v-if="linkError" tone="danger">{{ linkError }}</AAlert>
            <QueryError :mutation="link" />
            <div v-if="info.data.value?.oidc_enabled">
                <AButton
                    variant="tonal"
                    :leading-icon="IconLink"
                    :loading="link.isPending.value"
                    @click="connectSso"
                >
                    Connect SSO
                </AButton>
            </div>
        </IdentitiesCard>

        <ACard
            v-if="info.data.value?.password_login_enabled"
            :title="me.data.value?.has_password ? 'Change password' : 'Set password'"
        >
            <form :id="passwordFormId" novalidate @submit="passwordForm.onSubmit">
                <ATextField
                    v-bind="passwordForm.field('password')"
                    label="New password"
                    type="password"
                    autocomplete="new-password"
                />
                <QueryError :mutation="passwordForm.mutation" class="mt-3" />
            </form>
            <template #actions>
                <AButton
                    type="submit"
                    :form="passwordFormId"
                    :loading="passwordForm.mutation.isPending.value"
                >
                    Update password
                </AButton>
            </template>
        </ACard>
    </div>
</template>

<script setup lang="ts">
import { useQueryClient } from '@tanstack/vue-query'
import { useHead } from '@unhead/vue'
import { computed, useId, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { z } from 'zod'
import QueryError from '@/components/QueryError.vue'
import AAlert from '@/ui/AAlert.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import APageHeader from '@/ui/APageHeader.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconLink } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
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
const toast = useToast()
const detailsFormId = useId()
const passwordFormId = useId()

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
        username: z.string().min(3, 'Use at least 3 characters'),
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
        toast.show({ message: 'Saved your details' })
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
        password: z.string().min(8, 'Use at least 8 characters'),
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
        toast.show({ message: 'Password updated' })
    },
})
</script>
