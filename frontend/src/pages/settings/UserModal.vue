<template>
    <VDialog :model-value="open" @update:model-value="v => !v && close()" max-width="500">
        <VCard>
            <VCardTitle>{{ isNew ? 'Create User' : 'Edit User' }}</VCardTitle>
            <VCardText>
                <VForm @submit="form.onSubmit" class="space-y-4!">
                    <AInput :input="form.getInputProps('username')" label="Username" />
                    <AInput :input="form.getInputProps('email')" label="Email" type="email" />
                    <AInput
                        :input="form.getInputProps('password')"
                        :label="
                            isNew
                                ? 'Password (optional, leave blank for an SSO-only account)'
                                : 'New Password (leave blank to keep current)'
                        "
                        type="password"
                    />
                    <VCheckbox
                        :model-value="form.values.value.isAdmin"
                        @update:model-value="form.setValue('isAdmin', $event || false)"
                        label="Admin"
                        hide-details
                    />
                    <AQueryError :mutation="form.mutation" />
                    <div class="flex gap-2">
                        <VBtn
                            type="submit"
                            color="primary"
                            :loading="form.mutation.isPending.value"
                        >
                            {{ isNew ? 'Create' : 'Update' }}
                        </VBtn>
                        <VBtn variant="text" @click="close()"> Cancel </VBtn>
                        <VSpacer />
                        <VBtn
                            v-if="!isNew"
                            color="error"
                            variant="text"
                            :loading="deleteUser.isPending.value"
                            @click="handleDelete"
                        >
                            Delete
                        </VBtn>
                    </div>
                </VForm>

                <template v-if="!isNew">
                    <VDivider class="my-4" />
                    <IdentitiesCard :user-id="userId" flat />
                    <VForm @submit="linkForm.onSubmit" class="mt-2 space-y-2!">
                        <div class="grid grid-cols-1 gap-2 sm:grid-cols-3">
                            <VSelect
                                :model-value="linkForm.values.value.provider"
                                @update:model-value="linkForm.setValue('provider', $event)"
                                :items="['oidc', 'proxy']"
                                label="Provider"
                                density="compact"
                                hide-details
                            />
                            <AInput
                                :input="linkForm.getInputProps('issuer')"
                                label="Issuer"
                                density="compact"
                                :disabled="linkForm.values.value.provider === 'proxy'"
                            />
                            <AInput
                                :input="linkForm.getInputProps('subject')"
                                label="Subject"
                                density="compact"
                            />
                        </div>
                        <AQueryError :mutation="linkForm.mutation" />
                        <VBtn
                            type="submit"
                            variant="tonal"
                            size="small"
                            :loading="linkForm.mutation.isPending.value"
                        >
                            Link identity
                        </VBtn>
                    </VForm>
                </template>
            </VCardText>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { z } from 'zod'
import AInput from '@/components/AInput.vue'
import AQueryError from '@/components/AQueryError.vue'
import { usersApi } from '@/utils/api/users'
import { useForm } from '@/utils/forms'
import IdentitiesCard from './IdentitiesCard.vue'

const props = defineProps<{
    open: boolean
    close: () => void
    userId: string
}>()

const isNew = computed(() => props.userId === 'new')
const users = usersApi.useList()
const user = computed(() => users.data?.value?.find(u => u.id === props.userId))
const upsert = usersApi.useUpsert()
const deleteUser = usersApi.useDelete()
const linkIdentity = usersApi.useLinkIdentity(() => props.userId)

const form = useForm({
    schema: z.object({
        username: z.string().min(3),
        email: z.string(),
        password: z
            .string()
            .optional()
            .superRefine((val, ctx) => {
                if (val && val.length < 8) {
                    ctx.issues.push({
                        code: 'custom',
                        message: 'Password must be at least 8 characters long',
                        input: val,
                    })
                }
            }),
        isAdmin: z.boolean(),
    }),
    initialValues: {
        username: '',
        email: '',
        password: '',
        isAdmin: true,
    },
    onSubmit: async values => {
        await upsert.mutateAsync({
            id: isNew.value ? undefined : props.userId,
            username: values.username,
            email: values.email,
            password: values.password || undefined,
            permissions: values.isAdmin ? ['ADMIN'] : [],
        })
        props.close()
    },
})

const linkForm = useForm({
    schema: z.object({
        provider: z.enum(['oidc', 'proxy']),
        issuer: z.string(),
        subject: z.string().min(1),
    }),
    initialValues: { provider: 'oidc' as 'oidc' | 'proxy', issuer: '', subject: '' },
    onSubmit: async values => {
        await linkIdentity.mutateAsync({
            provider: values.provider,
            issuer: values.provider === 'proxy' ? '' : values.issuer,
            subject: values.subject,
        })
        linkForm.reset()
    },
})

watch(
    () => user.value,
    u => {
        if (u && !isNew.value) {
            form.setValues({
                username: u.username,
                email: u.email ?? '',
                password: '',
                isAdmin: u.permissions.includes('ADMIN'),
            })
        }
    },
    { immediate: true }
)

async function handleDelete() {
    if (isNew.value) return
    await deleteUser.mutateAsync(props.userId)
    props.close()
}
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './UserModal.vue'

export function showUserModal(userId: string): Promise<void> {
    return Modals.show(Self, { userId })
}
</script>
