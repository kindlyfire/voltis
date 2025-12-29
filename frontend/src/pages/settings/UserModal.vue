<template>
    <VDialog
        :model-value="modelValue"
        @update:model-value="$emit('update:modelValue', $event)"
        max-width="500"
    >
        <VCard>
            <VCardTitle>{{ isNew ? 'Create User' : 'Edit User' }}</VCardTitle>
            <VCardText>
                <VForm @submit="form.onSubmit" class="space-y-4!">
                    <AInput :input="form.getInputProps('username')" label="Username" />
                    <AInput
                        :input="form.getInputProps('password')"
                        :label="isNew ? 'Password' : 'New Password (leave blank to keep current)'"
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
                        <VBtn variant="text" @click="$emit('update:modelValue', false)">
                            Cancel
                        </VBtn>
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
            </VCardText>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { z } from 'zod'
import { useForm } from '@/utils/forms'
import { usersApi } from '@/utils/api/users'
import AInput from '@/components/AInput.vue'
import AQueryError from '@/components/AQueryError.vue'

const props = defineProps<{
    userId: string | null
    modelValue: boolean
}>()

const emit = defineEmits<{
    'update:modelValue': [boolean]
}>()

const isNew = computed(() => props.userId === 'new')
const users = usersApi.useList()
const user = computed(() => users.data?.value?.find(u => u.id === props.userId))
const upsert = usersApi.useUpsert()
const deleteUser = usersApi.useDelete()

const form = useForm({
    schema: z.object({
        username: z.string().min(3),
        password: z
            .string()
            .optional()
            .superRefine((val, ctx) => {
                if ((val && val.length < 8) || (isNew.value && !val)) {
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
        password: '',
        isAdmin: true,
    },
    onSubmit: async values => {
        await upsert.mutateAsync({
            id: isNew.value ? undefined : props.userId!,
            username: values.username,
            password: values.password || undefined,
            permissions: values.isAdmin ? ['ADMIN'] : [],
        })
        emit('update:modelValue', false)
    },
})

watch(
    () => props.userId,
    () => {
        form.reset()
        if (props.userId === 'new') {
            form.setValues({ username: '', password: '', isAdmin: false })
        } else if (user.value) {
            form.setValues({
                username: user.value.username,
                password: '',
                isAdmin: user.value.permissions.includes('ADMIN'),
            })
        }
    },
    { immediate: true }
)

watch(
    () => user.value,
    u => {
        if (u && props.userId !== 'new') {
            form.setValues({
                username: u.username,
                password: '',
                isAdmin: u.permissions.includes('ADMIN'),
            })
        }
    }
)

async function handleDelete() {
    if (!props.userId || isNew.value) return
    await deleteUser.mutateAsync(props.userId)
    emit('update:modelValue', false)
}
</script>
