<template>
	<div class="acontainer-xs">
		<UForm
			:schema="schema"
			:state="state"
			@submit="mLogin.mutate()"
			class="flex flex-col gap-4"
		>
			<h1 class="text-3xl font-bold">Login</h1>
			<div v-if="topMessage">
				{{ topMessage }}
			</div>

			<UFormGroup label="Email or username" name="emailOrUsername">
				<UInput v-model="state.emailOrUsername" :disabled="formDisabled" />
			</UFormGroup>

			<UFormGroup label="Password" name="password">
				<UInput
					v-model="state.password"
					:disabled="formDisabled"
					type="password"
				/>
			</UFormGroup>

			<div v-if="errorMessage" class="text-red-500">
				{{ errorMessage }}
			</div>

			<div>
				<UButton
					type="submit"
					:loading="mLogin.isPending.value"
					:disabled="formDisabled"
				>
					Log in
				</UButton>
			</div>
		</UForm>
	</div>
</template>

<script lang="ts" setup>
import { useMutation } from '@tanstack/vue-query'
import { trpc } from '../../plugins/trpc'
import { useUser } from '../../state/composables/queries'
import { queryClient } from '../../plugins/vue-query'
import { z } from 'zod'

const qMeta = trpc.meta.useQuery()
const meta = qMeta.data
const qUser = useUser()

const schema = z.object({
	emailOrUsername: z.string().min(3, 'Must be at least 3 characters'),
	password: z.string().min(8, 'Must be at least 8 characters')
})

const state = reactive({
	emailOrUsername: '',
	password: ''
}) satisfies z.output<typeof schema>

const mLogin = useMutation({
	async mutationFn() {
		const u = await trpc.auth.login.mutate({
			emailOrUsername: state.emailOrUsername,
			password: state.password
		})
		await qMeta.refresh()
		queryClient.setQueryData(['user'], u)
		await navigateTo('/')
	}
})

const errorMessage = computed(() => {
	const e = mLogin.error.value
	if (!e) return
	if (e.message === 'UNAUTHORIZED') {
		return 'Invalid email/username or password.'
	}
	return `${e.name}: ${e.message}`
})

const topMessage = computed(() => {
	if (qUser.data.value) {
		return 'You are already logged in.'
	}
})

const formDisabled = computed(() => {
	return !!qUser.data.value
})
</script>

<style></style>
