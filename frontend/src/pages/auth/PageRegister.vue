<template>
	<VContainer class="fill-height" fluid>
		<VRow justify="center">
			<VCol cols="12" sm="8" md="4">
				<VCard>
					<VCardTitle class="text-h5">Register</VCardTitle>
					<VCardText>
						<VForm @submit="onSubmit" class="space-y-4!">
							<AInput :input="getInputProps('username')" label="Username" />
							<AInput
								:input="getInputProps('password')"
								label="Password"
								type="password"
							/>
							<AInput
								:input="getInputProps('confirmPassword')"
								label="Confirm Password"
								type="password"
							/>
							<AMutationError :mutation="mutation" />
							<VBtn
								type="submit"
								color="primary"
								block
								:loading="mutation.isPending.value"
								class="mt-4"
							>
								Register
							</VBtn>
						</VForm>
					</VCardText>
					<VCardActions>
						<VSpacer />
						<RouterLink to="/auth/login">Already have an account?</RouterLink>
					</VCardActions>
				</VCard>
			</VCol>
		</VRow>
	</VContainer>
</template>

<script setup lang="ts">
import { z } from 'zod'
import { useForm } from '@/utils/forms'
import { authApi } from '@/utils/api/auth'
import AInput from '@/components/AInput.vue'
import AMutationError from '@/components/AMutationError.vue'
import { useRouter } from 'vue-router'

const register = authApi.useRegister()
const router = useRouter()

const schema = z
	.object({
		username: z.string().min(3),
		password: z.string().min(8),
		confirmPassword: z.string(),
	})
	.refine(data => data.password === data.confirmPassword, {
		message: 'Passwords do not match',
		path: ['confirmPassword'],
	})

const { getInputProps, onSubmit, mutation } = useForm({
	schema,
	initialValues: {
		username: '',
		password: '',
		confirmPassword: '',
	},
	onSubmit: async values => {
		await register.mutateAsync({
			username: values.username,
			password: values.password,
		})
		router.push('/')
	},
})
</script>
