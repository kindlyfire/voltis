<template>
    <ADialog
        :open="open"
        :title="isNew ? 'Create user' : 'Edit user'"
        @update:open="v => !v && close()"
    >
        <div class="flex flex-col gap-5">
            <form :id="formId" novalidate class="flex flex-col gap-3" @submit="form.onSubmit">
                <ATextField
                    v-bind="form.field('username')"
                    label="Username"
                    autocomplete="off"
                    autofocus
                />
                <ATextField
                    v-bind="form.field('email')"
                    label="Email"
                    type="email"
                    autocomplete="off"
                />
                <ATextField
                    v-bind="form.field('password')"
                    :label="isNew ? 'Password' : 'New password'"
                    :hint="
                        isNew
                            ? 'Optional. Leave it blank for an SSO-only account.'
                            : 'Leave it blank to keep the current password.'
                    "
                    type="password"
                    autocomplete="new-password"
                />
                <ACheckbox v-bind="form.field('isAdmin')" label="Admin" />
                <QueryError :mutation="form.mutation" />
                <QueryError :mutation="deleteUser" />
            </form>

            <template v-if="!isNew">
                <IdentitiesCard :user-id="userId" compact>
                    <form novalidate class="flex flex-col gap-3" @submit="linkForm.onSubmit">
                        <div class="grid grid-cols-1 items-start gap-2 sm:grid-cols-3">
                            <ASelect
                                :model-value="linkForm.values.value.provider"
                                :options="providerOptions"
                                label="Provider"
                                size="sm"
                                @update:model-value="v => v && linkForm.setValue('provider', v)"
                            />
                            <ATextField
                                v-bind="linkForm.field('issuer')"
                                label="Issuer"
                                size="sm"
                                :disabled="linkForm.values.value.provider === 'proxy'"
                            />
                            <ATextField
                                v-bind="linkForm.field('subject')"
                                label="Subject"
                                size="sm"
                            />
                        </div>
                        <QueryError :mutation="linkForm.mutation" />
                        <div>
                            <AButton
                                type="submit"
                                variant="tonal"
                                size="sm"
                                :leading-icon="IconLink"
                                :loading="linkForm.mutation.isPending.value"
                            >
                                Link identity
                            </AButton>
                        </div>
                    </form>
                </IdentitiesCard>
            </template>
        </div>

        <template #actions>
            <AButton
                v-if="!isNew"
                variant="text"
                tone="danger"
                class="mr-auto"
                :loading="deleteUser.isPending.value"
                @click="handleDelete"
            >
                Delete
            </AButton>
            <AButton variant="text" tone="neutral" @click="close()">Cancel</AButton>
            <AButton type="submit" :form="formId" :loading="form.mutation.isPending.value">
                {{ isNew ? 'Create' : 'Save' }}
            </AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { computed, useId, watch } from 'vue'
import { z } from 'zod'
import { showConfirmModal } from '@/components/ConfirmModal.vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ACheckbox from '@/ui/ACheckbox.vue'
import ADialog from '@/ui/ADialog.vue'
import ASelect from '@/ui/ASelect.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconLink } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
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
const formId = useId()
const toast = useToast()
const providerOptions = [
    { value: 'oidc', label: 'SSO' },
    { value: 'proxy', label: 'Proxy' },
] as const

const form = useForm({
    schema: z.object({
        username: z.string().min(3, 'Use at least 3 characters'),
        email: z.string(),
        password: z.string().refine(v => !v || v.length >= 8, 'Use at least 8 characters'),
        isAdmin: z.boolean(),
    }),
    initialValues: {
        username: '',
        email: '',
        password: '',
        isAdmin: false,
    },
    onSubmit: async values => {
        await upsert.mutateAsync({
            id: isNew.value ? undefined : props.userId,
            username: values.username,
            email: values.email,
            password: values.password || undefined,
            permissions: values.isAdmin ? ['ADMIN'] : [],
        })
        toast.show({ message: isNew.value ? `Created ${values.username}` : 'User saved' })
        props.close()
    },
})

const linkForm = useForm({
    schema: z.object({
        provider: z.enum(['oidc', 'proxy']),
        issuer: z.string(),
        subject: z.string().trim().min(1, 'Subject is required'),
    }),
    initialValues: { provider: 'oidc' as 'oidc' | 'proxy', issuer: '', subject: '' },
    onSubmit: async values => {
        await linkIdentity.mutateAsync({
            provider: values.provider,
            issuer: values.provider === 'proxy' ? '' : values.issuer,
            subject: values.subject,
        })
        linkForm.reset()
        toast.show({ message: `Linked ${values.subject}` })
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
    const name = user.value?.username ?? 'this user'
    const confirmed = await showConfirmModal({
        title: 'Delete user?',
        message: `${name} and their reading data, lists and sessions will be deleted.`,
        confirmText: 'Delete',
        tone: 'danger',
    })
    if (!confirmed) return
    await deleteUser.mutateAsync(props.userId)
    toast.show({ message: `Deleted ${name}` })
    props.close()
}
</script>

<script lang="ts">
import { Modals, type ShowOptions } from '@/utils/modals'
import Self from './UserModal.vue'

export function showUserModal(userId: string, options?: ShowOptions): Promise<void> {
    return Modals.show(Self, { userId }, options)
}
</script>
