<template>
	<AMainWrapper>
		<template #side>
			<AdminPagesSidebar />
		</template>
		<template #main>
			<PageTitle title="Summary" pagetitle="Summary (Admin)" />
		</template>
	</AMainWrapper>
</template>

<script lang="ts" setup>
import AdminPagesSidebar from '../../components/admin/AdminPagesSidebar.vue'
</script>

<style></style>
