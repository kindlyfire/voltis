<template>
    <AuthCard
        :title="
            state === 'expired' || state === 'unavailable'
                ? 'Nothing to finish'
                : 'Finish signing in'
        "
        :loading="state === 'loading'"
    >
        <template v-if="active">
            <div v-if="showConfirm" class="flex flex-col gap-4">
                <p class="text-fg-muted text-sm leading-normal">
                    The account <strong class="text-fg">{{ active.match_username }}</strong>
                    matches this sign-in. Enter its password to link them.
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
                        <AButton
                            v-if="active.can_create"
                            variant="text"
                            tone="neutral"
                            @click="declined = true"
                        >
                            Not my account
                        </AButton>
                        <AButton
                            v-else
                            variant="text"
                            tone="neutral"
                            :to="loginLink(active.redirect)"
                        >
                            Back to login
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
                            v-if="active.needs === 'confirm'"
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

        <p
            v-else-if="state !== 'loading' && state !== 'active'"
            class="text-fg-muted text-sm leading-normal"
        >
            {{ messages[state] }}
        </p>
        <template v-if="ended" #footer>
            <AButton
                :variant="state === 'failed' ? 'text' : undefined"
                :tone="state === 'failed' ? 'neutral' : undefined"
                :to="loginLink(qPending.data.value?.redirect)"
            >
                Back to login
            </AButton>
            <AButton
                v-if="state === 'failed'"
                :loading="qPending.isFetching.value"
                @click="qPending.refetch()"
            >
                Retry
            </AButton>
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
import { RequestError } from '@/utils/fetch'
import { useForm } from '@/utils/forms'
import { redirectQuery, safeRedirect } from '@/utils/redirect'
import AuthCard from './AuthCard.vue'

useHead({ title: 'Finish signing in' })

const router = useRouter()
const queryClient = useQueryClient()
const qPending = oidcApi.usePending()
const confirm = oidcApi.useConfirm()
const chooseUsername = oidcApi.useChooseUsername()

const state = computed(() => {
    const { data, error, isLoading } = qPending
    if (isLoading.value) return 'loading'
    // TanStack Query keeps stale data when a refetch fails, so a 404 must override it.
    if (error.value instanceof RequestError && error.value.response?.status === 404)
        return 'expired'
    // Any other failed refetch gets Retry rather than a form built on stale data.
    if (error.value) return 'failed'
    // The picker is gone once auto-create is turned off after the callback.
    if (data.value) {
        return data.value.can_create || data.value.needs === 'confirm' ? 'active' : 'unavailable'
    }
    return 'expired'
})
const messages = {
    failed: "Couldn't load this sign-in.",
    unavailable: 'This sign-in is no longer available.',
    expired: 'This sign-in is no longer waiting. Start again from the login page.',
}
const ended = computed(() => state.value !== 'loading' && state.value !== 'active')
const active = computed(() => (state.value === 'active' ? qPending.data.value : undefined))
const declined = ref(false)
const showConfirm = computed(
    () => active.value?.needs === 'confirm' && !(declined.value && active.value.can_create)
)

const loginLink = (redirect: string | undefined) => ({
    path: '/auth/login',
    query: { local: '1', ...redirectQuery(redirect) },
})

// The mutations consume the pending row, so callers read the target before awaiting them.
const redirectTarget = () => safeRedirect(active.value?.redirect) ?? '/'

async function signedIn(target: string) {
    await queryClient.refetchQueries({ queryKey: ['users', 'me'] })
    router.replace(target)
}

const confirmForm = useForm({
    schema: z.object({ password: z.string().min(1, 'Enter your password') }),
    initialValues: { password: '' },
    onSubmit: async values => {
        const target = redirectTarget()
        try {
            await confirm.mutateAsync(values.password)
        } catch (err) {
            // A wrong password can use up the row; refetching shows the expired state.
            qPending.refetch()
            throw err
        }
        await signedIn(target)
    },
})

const usernameForm = useForm({
    schema: z.object({ username: z.string().min(2, 'Use at least 2 characters') }),
    initialValues: { username: '' },
    onSubmit: async values => {
        const target = redirectTarget()
        await chooseUsername.mutateAsync(values.username)
        await signedIn(target)
    },
})

watch(
    () => active.value?.username,
    suggested => {
        if (suggested && !usernameForm.values.value.username) {
            usernameForm.setValues({ username: suggested })
        }
    },
    { immediate: true }
)
</script>
