<template>
	<AMainWrapper>
		<template #side>
			<UserPagesSidebar />
		</template>
		<template #main>
			<PageTitle title="Account" />

			<UFormGroup label="Username" name="username" size="lg">
				<UInput :model-value="user.username" disabled />
			</UFormGroup>

			<UFormGroup label="Email" name="email" size="lg">
				<UInput :model-value="user.email" disabled />
			</UFormGroup>
		</template>
	</AMainWrapper>
</template>

<script lang="ts" setup>
import UserPagesSidebar from '../../components/user/UserPagesSidebar.vue'
import { useUser } from '../../state/composables/queries'

const qUser = useUser()
const user = computed(() => qUser.data.value!)
</script>

<style></style>
