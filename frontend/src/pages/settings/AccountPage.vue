<template>
	<VContainer>
		<h1 class="text-h4 mb-6">Account</h1>

		<VCard class="mb-6">
			<VCardTitle>User Details</VCardTitle>
			<VCardText>
				<VForm @submit="detailsForm.onSubmit">
					<AInput :input="detailsForm.getInputProps('username')" label="Username" />
					<AMutationError :mutation="detailsForm.mutation" />
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
					<AMutationError :mutation="passwordForm.mutation" />
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
import { z } from 'zod'
import { watch } from 'vue'
import { useForm } from '@/utils/forms'
import { usersApi } from '@/utils/api/users'
import AInput from '@/components/AInput.vue'
import AMutationError from '@/components/AMutationError.vue'
import { useHead } from '@unhead/vue'

useHead({
	title: 'Account',
})

const me = usersApi.useMe()
const upsert = usersApi.useUpsert()

const detailsForm = useForm({
	schema: z.object({
		username: z.string().min(3),
	}),
	initialValues: {
		username: '',
	},
	onSubmit: async values => {
		await upsert.mutateAsync({
			id: me.data.value!.id,
			username: values.username,
			permissions: me.data.value!.permissions,
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
			id: _me.id,
			username: _me.username,
			password: values.password,
			permissions: _me.permissions,
		})
		passwordForm.reset()
	},
})
</script>
