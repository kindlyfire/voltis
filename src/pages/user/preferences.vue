<template>
	<AMainWrapper>
		<template #side>
			<UserPagesSidebar />
		</template>
		<template #main>
			<div class="text-5xl font-bold">Preferences</div>
		</template>
	</AMainWrapper>
</template>

<script lang="ts" setup>
import UserPagesSidebar from '../../components/user/UserPagesSidebar.vue'
</script>

<style></style>
