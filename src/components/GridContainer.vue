<template>
	<div class="GridContainer">
		<div class="GridInner grid gap-2">
			<slot v-for="item in items" v-bind="{ item }" />
		</div>
	</div>
</template>

<script lang="ts" setup generic="T">
const props = defineProps<{
	items: T[]
}>()
const emit = defineEmits<{}>()
</script>

<style>
.GridContainer {
	container: grid-container / inline-size;
}

.GridInner {
	grid-template-columns: repeat(2, minmax(0, 1fr));
}

@container grid-container (min-width: 400px) {
	.GridInner {
		grid-template-columns: repeat(3, minmax(0, 1fr));
	}
}
@container grid-container (min-width: 640px) {
	.GridInner {
		grid-template-columns: repeat(4, minmax(0, 1fr));
	}
}
@container grid-container (min-width: 768px) {
	.GridInner {
		grid-template-columns: repeat(5, minmax(0, 1fr));
	}
}
@container grid-container (min-width: 1024px) {
	.GridInner {
		grid-template-columns: repeat(7, minmax(0, 1fr));
	}
}
@container grid-container (min-width: 1280px) {
	.GridInner {
		grid-template-columns: repeat(8, minmax(0, 1fr));
	}
}
</style>
