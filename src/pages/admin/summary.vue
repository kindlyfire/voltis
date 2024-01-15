<template>
	<AMainWrapper>
		<template #side>
			<AdminPagesSidebar />
		</template>
		<template #main>
			<div class="text-5xl font-bold">Summary</div>
		</template>
	</AMainWrapper>
</template>

<script lang="ts" setup>
import AdminPagesSidebar from '../../components/admin/AdminPagesSidebar.vue'
</script>

<style></style>
