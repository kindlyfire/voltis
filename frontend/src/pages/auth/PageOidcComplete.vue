<template>
    <VContainer>
        <VRow justify="center">
            <VCol cols="12" sm="8" md="5">
                <VCard v-if="qPending.isLoading.value">
                    <VCardText class="flex justify-center py-8">
                        <VProgressCircular indeterminate />
                    </VCardText>
                </VCard>

                <VCard v-else-if="pending">
                    <VCardTitle class="text-h5">Finish signing in</VCardTitle>

                    <VCardText v-if="showConfirm">
                        <p class="mb-4">
                            An account named <strong>{{ pending.match_username }}</strong> already
                            exists. Enter its password to link it to your provider account, or
                            choose a different username.
                        </p>
                        <VForm @submit="confirmForm.onSubmit" class="space-y-4!">
                            <AInput
                                :input="confirmForm.getInputProps('password')"
                                label="Password"
                                type="password"
                                autofocus
                            />
                            <AQueryError :mutation="confirmForm.mutation" />
                            <div class="flex gap-2">
                                <VBtn
                                    type="submit"
                                    color="primary"
                                    :loading="confirmForm.mutation.isPending.value"
                                >
                                    Link account
                                </VBtn>
                                <VBtn variant="text" @click="declined = true">Not my account</VBtn>
                            </div>
                        </VForm>
                    </VCardText>

                    <VCardText v-else>
                        <p class="mb-4">Pick a username for your new account.</p>
                        <VForm @submit="usernameForm.onSubmit" class="space-y-4!">
                            <AInput
                                :input="usernameForm.getInputProps('username')"
                                label="Username"
                                autofocus
                            />
                            <AQueryError :mutation="usernameForm.mutation" />
                            <div class="flex gap-2">
                                <VBtn
                                    type="submit"
                                    color="primary"
                                    :loading="usernameForm.mutation.isPending.value"
                                >
                                    Create account
                                </VBtn>
                                <VBtn
                                    v-if="pending.needs === 'confirm'"
                                    variant="text"
                                    @click="declined = false"
                                >
                                    Back
                                </VBtn>
                            </div>
                        </VForm>
                    </VCardText>
                </VCard>

                <VCard v-else>
                    <VCardTitle class="text-h5">Nothing to finish</VCardTitle>
                    <VCardText>
                        This sign-in is no longer waiting. Start again from the login page.
                    </VCardText>
                    <VCardActions>
                        <VSpacer />
                        <VBtn color="primary" to="/auth/login?local=1">Back to login</VBtn>
                    </VCardActions>
                </VCard>
            </VCol>
        </VRow>
    </VContainer>
</template>

<script setup lang="ts">
import { useQueryClient } from '@tanstack/vue-query'
import { useHead } from '@unhead/vue'
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { z } from 'zod'
import AInput from '@/components/AInput.vue'
import AQueryError from '@/components/AQueryError.vue'
import { oidcApi } from '@/utils/api/oidc'
import { useForm } from '@/utils/forms'

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
    schema: z.object({ password: z.string().min(1) }),
    initialValues: { password: '' },
    onSubmit: async values => {
        await confirm.mutateAsync(values.password)
        await signedIn()
    },
})

const usernameForm = useForm({
    schema: z.object({ username: z.string().min(2) }),
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
