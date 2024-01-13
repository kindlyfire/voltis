<template>
	<div class="acontainer-xs">
		<UForm
			:schema="schema"
			:state="state"
			@submit="mCreateUser.mutate()"
			class="flex flex-col gap-4"
		>
			<h1 class="text-3xl font-bold">Register</h1>
			<div>
				{{ topMessage }}
			</div>

			<UFormGroup label="Username" name="username">
				<UInput v-model="state.username" :disabled="formDisabled" />
			</UFormGroup>

			<UFormGroup label="Email" name="email">
				<UInput v-model="state.email" :disabled="formDisabled" />
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
					:loading="mCreateUser.isPending.value"
					:disabled="formDisabled"
				>
					Register
				</UButton>
			</div>
		</UForm>
	</div>
</template>

<script lang="ts" setup>
import { useMutation } from '@tanstack/vue-query'
import { trpc } from '../../plugins/trpc'
import { useUser } from '../../state/composables/use-user'
import { queryClient } from '../../plugins/vue-query'
import { z } from 'zod'

const qMeta = trpc.meta.useQuery()
const meta = qMeta.data
const qUser = useUser()

const schema = z.object({
	username: z.string().min(3, 'Must be at least 3 characters'),
	email: z.string().email(),
	password: z.string().min(8, 'Must be at least 8 characters')
})

const state = reactive({
	username: '',
	email: '',
	password: ''
}) satisfies z.output<typeof schema>

const mCreateUser = useMutation({
	async mutationFn() {
		const u = await trpc.auth.register.mutate({
			username: state.username,
			email: state.email,
			password: state.password
		})
		await qMeta.refresh()
		queryClient.setQueryData(['user'], u)
		await navigateTo('/')
	}
})

const errorMessage = computed(() => {
	const e = mCreateUser.error.value
	if (!e) return
	return `${e.name}: ${e.message}`
})

const topMessage = computed(() => {
	const m = meta.value
	if (qUser.data.value) {
		return 'You are already logged in.'
	} else if (m?.forceUserCreation) {
		return 'To continue setting up Voltis, please create an administrator account here.'
	} else if (m && !m.registrationsEnabled) {
		return 'Registrations are currently disabled. Please contact the instance administrator to request account creation.'
	}
})

const formDisabled = computed(() => {
	return !!qUser.data.value || !meta.value?.registrationsEnabled
})
</script>

<style></style>
