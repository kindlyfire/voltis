<template>
	<div class="acontainer">
		<div class="flex flex-row gap-4">
			<UserPagesSidebar />
			<div class="flex flex-col gap-4 grow">
				<div class="text-5xl font-bold">Account</div>

				<UFormGroup label="Username" name="username" size="lg">
					<UInput :model-value="user.username" disabled />
				</UFormGroup>

				<UFormGroup label="Email" name="email" size="lg">
					<UInput :model-value="user.email" disabled />
				</UFormGroup>
			</div>
		</div>
	</div>
</template>

<script lang="ts" setup>
import UserPagesSidebar from '../../components/user/UserPagesSidebar.vue'
import { useUser } from '../../state/composables/queries'

const qUser = useUser()
const user = computed(() => qUser.data.value!)
</script>

<style></style>
