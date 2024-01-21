<template>
	<AMainWrapper>
		<template #side>
			<UserPagesSidebar />
		</template>
		<template #main>
			<UForm
				:schema="schema"
				:state="state"
				@submit="mSave.mutate()"
				class="flex flex-col gap-4"
			>
				<PageTitle title="Account" />

				<UFormGroup label="Username" name="username" size="lg">
					<UInput v-model="state.username" :disabled="formDisabled" />
				</UFormGroup>

				<UFormGroup label="Email" name="email" size="lg">
					<UInput v-model="state.email" :disabled="formDisabled" />
				</UFormGroup>

				<UFormGroup label="Password" name="password" size="lg">
					<UInput
						v-model="state.password"
						:disabled="formDisabled"
						type="password"
						placeholder="Leave blank to keep unchanged"
					/>
				</UFormGroup>

				<div v-if="errorMessage" class="text-red-500">
					{{ errorMessage }}
				</div>

				<div>
					<UButton
						type="submit"
						:loading="mSave.isPending.value"
						:disabled="formDisabled"
					>
						Save
					</UButton>
				</div>
			</UForm>
		</template>
	</AMainWrapper>
</template>

<script lang="ts" setup>
import { z } from 'zod'
import UserPagesSidebar from '../../components/user/UserPagesSidebar.vue'
import { useUser } from '../../state/composables/queries'
import { useMutation } from '@tanstack/vue-query'
import { trpc } from '../../plugins/trpc'

const qUser = useUser()
const user = computed(() => qUser.data.value!)
const toast = useToast()

const schema = z.object({
	username: z.string().min(3, 'Must be at least 3 characters'),
	email: z.string().email(),
	password: z.string().min(8, 'Must be at least 8 characters').or(z.literal(''))
})

const state = reactive({
	username: user.value.username!,
	email: user.value.email!,
	password: ''
}) satisfies z.output<typeof schema>

const mSave = useMutation({
	async mutationFn() {
		await trpc.user.update.mutate({
			email: state.email,
			username: state.username,
			password: state.password || undefined
		})
		await qUser.refetch()
		toast.add({
			title: 'Account updated'
		})
	}
})
const errorMessage = computed(() => {
	const e = mSave.error.value
	if (!e) return
	return `${e.name}: ${e.message}`
})

const formDisabled = computed(() => {
	return mSave.isPending.value
})
</script>

<style></style>
