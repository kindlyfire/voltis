<template>
    <VContainer>
        <h1 class="text-h4 mb-6">Account</h1>

        <VCard class="mb-6">
            <VCardTitle>User Details</VCardTitle>
            <VCardText>
                <VForm @submit="detailsForm.onSubmit">
                    <AInput :input="detailsForm.getInputProps('username')" label="Username" />
                    <AQueryError :mutation="detailsForm.mutation" />
                    <VBtn
                        type="submit"
                        color="primary"
                        :loading="detailsForm.mutation.isPending.value"
                        class="mt-4"
                    >
                        Update Username
                    </VBtn>
                </VForm>
            </VCardText>
        </VCard>

        <VCard>
            <VCardTitle>Change Password</VCardTitle>
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
import { useHead } from '@unhead/vue'
import { watch } from 'vue'
import { z } from 'zod'
import AInput from '@/components/AInput.vue'
import AQueryError from '@/components/AQueryError.vue'
import { usersApi } from '@/utils/api/users'
import { useForm } from '@/utils/forms'

useHead({
    title: 'Account',
})

const me = usersApi.useMe()
const upsert = usersApi.useUpdateMe()

const detailsForm = useForm({
    schema: z.object({
        username: z.string().min(3),
    }),
    initialValues: {
        username: '',
    },
    onSubmit: async values => {
        await upsert.mutateAsync({
            username: values.username,
        })
    },
})

watch(
    () => me.data.value,
    user => {
        if (user) {
            detailsForm.setValues({
                username: user.username,
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
