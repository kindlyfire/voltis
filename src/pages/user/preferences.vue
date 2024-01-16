<template>
	<AMainWrapper>
		<template #side>
			<UserPagesSidebar />
		</template>
		<template #main>
			<PageTitle title="Preferences" />
		</template>
	</AMainWrapper>
</template>

<script lang="ts" setup>
import UserPagesSidebar from '../../components/user/UserPagesSidebar.vue'
</script>

<style></style>
