<template>
	<div class="acontainer flex flex-col gap-4">
		<div class="flex flex-row gap-4">
			<div class="w-[300px] shrink-0 flex flex-col gap-2">
				<slot name="side" />
			</div>
			<div class="flex flex-col gap-4 grow">
				<slot name="main" />
			</div>
		</div>
	</div>
</template>

<script lang="ts" setup></script>

<style></style>
