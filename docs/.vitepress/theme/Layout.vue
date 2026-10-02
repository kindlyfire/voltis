<script setup lang="ts">
import type PhotoSwipeLightbox from 'photoswipe/lightbox'
import { useRoute } from 'vitepress'
import DefaultTheme, { VPImage } from 'vitepress/theme-without-fonts'
import { onMounted, onUnmounted, watch } from 'vue'

const route = useRoute()
let lightbox: PhotoSwipeLightbox | undefined
let generation = 0

// Every linked image on the page is one gallery, so next/prev crosses the separate grids.
async function initLightbox() {
	const current = ++generation
	lightbox?.destroy()
	lightbox = undefined
	if (!document.querySelector('.vp-doc a:has(> img)')) return

	const { default: Lightbox } = await import('photoswipe/lightbox')
	if (current !== generation) return
	lightbox = new Lightbox({
		gallery: '.vp-doc',
		children: 'a:has(> img)',
		pswpModule: () => import('photoswipe'),
		bgOpacity: 0.92,
		// Keeps the controls on the backdrop rather than over the screenshot.
		paddingFn: viewport =>
			viewport.x < 768
				? { top: 48, bottom: 16, left: 0, right: 0 }
				: { top: 56, bottom: 24, left: 72, right: 72 },
	})
	// A themed screenshot holds both captures; the slide is the visible one, and so is the
	// thumbnail the open and close animations start from.
	const visibleImg = (link: HTMLElement | undefined) =>
		[...(link?.querySelectorAll('img') ?? [])].find(i => i.checkVisibility())
	lightbox.addFilter(
		'thumbEl',
		(thumb, data) => visibleImg(data.element) ?? (thumb as HTMLElement)
	)
	// The thumbnails are the full-size files, so their natural size is the slide's.
	lightbox.addFilter('domItemData', (data, _el, link) => {
		const img = visibleImg(link)
		if (img) {
			data.src = img.currentSrc
			data.width = img.naturalWidth || img.width
			data.height = img.naturalHeight || img.height
			data.msrc = img.currentSrc
			data.alt = img.alt
		}
		return data
	})
	lightbox.init()
}

onMounted(initLightbox)
watch(
	() => route.path,
	() => void initLightbox(),
	{ flush: 'post' }
)
onUnmounted(() => {
	generation++
	lightbox?.destroy()
})
</script>

<template>
	<DefaultTheme.Layout>
		<template #home-hero-after>
			<div class="hero-shot">
				<VPImage
					class="hero-shot-img"
					:image="{
						light: '/screenshots/comic_view.jpg',
						dark: '/screenshots/comic_view_dark.jpg',
						alt: 'Voltis comic view',
					}"
				/>
			</div>
		</template>
	</DefaultTheme.Layout>
</template>
