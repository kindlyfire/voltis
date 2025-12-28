<template>
	<div class="carousel-item" role="listitem">
		<slot />
	</div>
</template>

<style scoped>
.carousel-item {
	scroll-snap-align: start;
}
</style>
