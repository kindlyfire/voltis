<template>
	<VContainer class="fill-height" fluid>
		<VRow justify="center">
			<VCol cols="12" sm="8" md="4">
				<VCard>
					<VCardTitle class="text-h5">Login</VCardTitle>
					<VCardText>
						<VForm @submit="onSubmit" class="space-y-4!">
							<AInput :input="getInputProps('username')" label="Username" autofocus />
							<AInput
								:input="getInputProps('password')"
								label="Password"
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
								Login
							</VBtn>
						</VForm>
					</VCardText>
					<VCardActions>
						<VSpacer />
						<RouterLink to="/auth/register">Don't have an account?</RouterLink>
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
import { useHead } from '@unhead/vue'
import { useQueryClient } from '@tanstack/vue-query'

useHead({
	title: 'Login',
})

const login = authApi.useLogin()
const router = useRouter()
const queryClient = useQueryClient()

const schema = z.object({
	username: z.string().min(1),
	password: z.string().min(1),
})

const { getInputProps, onSubmit, mutation } = useForm({
	schema,
	initialValues: {
		username: '',
		password: '',
	},
	onSubmit: async values => {
		await login.mutateAsync({
			username: values.username,
			password: values.password,
		})
		await queryClient.refetchQueries({
			queryKey: ['users', 'me'],
		})
		router.push('/')
	},
})
</script>
