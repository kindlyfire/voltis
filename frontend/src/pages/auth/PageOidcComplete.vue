<template>
    <AuthCard
        :title="qPending.isLoading.value || pending ? 'Finish signing in' : 'Nothing to finish'"
        :loading="qPending.isLoading.value"
    >
        <template v-if="pending">
            <div v-if="showConfirm" class="flex flex-col gap-4">
                <p class="text-fg-muted text-sm leading-normal">
                    An account named <strong class="text-fg">{{ pending.match_username }}</strong>
                    already exists. Enter its password to link it to your provider account, or
                    choose a different username.
                </p>
                <form novalidate class="flex flex-col gap-4" @submit="confirmForm.onSubmit">
                    <ATextField
                        v-bind="confirmForm.field('password')"
                        label="Password"
                        type="password"
                        autocomplete="current-password"
                        autofocus
                    />
                    <QueryError :mutation="confirmForm.mutation" />
                    <div class="flex flex-wrap justify-end gap-2">
                        <AButton variant="text" tone="neutral" @click="declined = true">
                            Not my account
                        </AButton>
                        <AButton type="submit" :loading="confirmForm.mutation.isPending.value">
                            Link account
                        </AButton>
                    </div>
                </form>
            </div>

            <div v-else class="flex flex-col gap-4">
                <p class="text-fg-muted text-sm leading-normal">
                    Pick a username for your new account.
                </p>
                <form novalidate class="flex flex-col gap-4" @submit="usernameForm.onSubmit">
                    <ATextField
                        v-bind="usernameForm.field('username')"
                        label="Username"
                        autocomplete="username"
                        autofocus
                    />
                    <QueryError :mutation="usernameForm.mutation" />
                    <div class="flex flex-wrap justify-end gap-2">
                        <AButton
                            v-if="pending.needs === 'confirm'"
                            variant="text"
                            tone="neutral"
                            @click="declined = false"
                        >
                            Back
                        </AButton>
                        <AButton type="submit" :loading="usernameForm.mutation.isPending.value">
                            Create account
                        </AButton>
                    </div>
                </form>
            </div>
        </template>

        <p v-else class="text-fg-muted text-sm leading-normal">
            This sign-in is no longer waiting. Start again from the login page.
        </p>
        <template v-if="!qPending.isLoading.value && !pending" #footer>
            <AButton to="/auth/login?local=1">Back to login</AButton>
        </template>
    </AuthCard>
</template>

<script setup lang="ts">
import { useQueryClient } from '@tanstack/vue-query'
import { useHead } from '@unhead/vue'
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { z } from 'zod'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ATextField from '@/ui/ATextField.vue'
import { oidcApi } from '@/utils/api/oidc'
import { useForm } from '@/utils/forms'
import AuthCard from './AuthCard.vue'

useHead({ title: 'Finish signing in' })

const router = useRouter()
const queryClient = useQueryClient()
const qPending = oidcApi.usePending()
const confirm = oidcApi.useConfirm()
const chooseUsername = oidcApi.useChooseUsername()

const pending = computed(() => qPending.data.value)
const declined = ref(false)
const showConfirm = computed(() => pending.value?.needs === 'confirm' && !declined.value)

async function signedIn() {
    await queryClient.refetchQueries({ queryKey: ['users', 'me'] })
    router.push('/')
}

const confirmForm = useForm({
    schema: z.object({ password: z.string().min(1, 'Enter your password') }),
    initialValues: { password: '' },
    onSubmit: async values => {
        await confirm.mutateAsync(values.password)
        await signedIn()
    },
})

const usernameForm = useForm({
    schema: z.object({ username: z.string().min(2, 'Use at least 2 characters') }),
    initialValues: { username: '' },
    onSubmit: async values => {
        await chooseUsername.mutateAsync(values.username)
        await signedIn()
    },
})

watch(
    () => pending.value?.username,
    suggested => {
        if (suggested && !usernameForm.values.value.username) {
            usernameForm.setValues({ username: suggested })
        }
    },
    { immediate: true }
)
</script>
