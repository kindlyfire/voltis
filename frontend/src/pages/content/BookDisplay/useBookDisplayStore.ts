import { contentApi } from '@/utils/api/content'
import { useLocalStorage } from '@/utils/localStorage'
import { acceptHMRUpdate, defineStore } from 'pinia'
import { computed, markRaw, readonly, ref } from 'vue'

interface BookDisplaySettings {
	showHidden: Array<{
		id: string
		dt: string
	}>
}

function parseBookDisplaySettings(v: any): BookDisplaySettings {
	const defaults: BookDisplaySettings = { showHidden: [] }
	if (typeof v !== 'object' || v === null) return defaults
	const final = {
		showHidden: (Array.isArray(v.showHidden) ? (v.showHidden as any[]) : []).filter(item => {
			if (
				!(
					typeof item === 'object' &&
					item !== null &&
					typeof item.id === 'string' &&
					typeof item.dt === 'string'
				)
			)
				return false

			// If date is over a month old, remove it. Otherwise update it.
			const dt = new Date(item.dt)
			if (isNaN(dt.getTime())) return false
			const now = new Date()
			const diff = now.getTime() - dt.getTime()
			const oneMonth = 30 * 24 * 60 * 60 * 1000
			if (diff > oneMonth) return false

			item.dt = now.toISOString()
			return true
		}),
	}
	return final
}

export const useBookDisplayStore = defineStore('book-display', () => {
	const { value: settings } = useLocalStorage('reader:books', parseBookDisplaySettings)
	const contentId = ref(null as string | null)
	const chapterHref = ref(null as string | null)

	const qContent = contentApi.useGet(() => contentId.value)
	const qChapters = contentApi.useBookChapters(() => contentId.value)
	const qChapterContent = contentApi.useBookChapter(() => contentId.value, chapterHref)

	function setContentId(id: string) {
		contentId.value = id
	}

	function setChapterHref(href: string | null) {
		chapterHref.value = href
	}

	function setShowHidden(contentId: string, show: boolean) {
		const index = settings.value.showHidden.findIndex(item => item.id === contentId)
		if (show && index === -1) {
			settings.value.showHidden.push({ id: contentId, dt: new Date().toISOString() })
		} else if (!show && index !== -1) {
			settings.value.showHidden.splice(index, 1)
		}
	}

	const hasHiddenChapters = computed(() => qChapters.data.value?.some(ch => !ch.linear) ?? false)
	const showHiddenChapters = computed(() => {
		return settings.value.showHidden.some(v => v.id === contentId.value)
	})

	const visibleChapters = computed(() => {
		if (!qChapters.data.value) return []
		if (showHiddenChapters.value) return qChapters.data.value
		return qChapters.data.value.filter(ch => ch.linear)
	})

	return {
		settings,
		contentId: readonly(contentId),
		chapterHref: readonly(chapterHref),
		qChapters: markRaw(qChapters),
		chapters: qChapters.data,
		qContent: markRaw(qContent),
		content: qContent.data,
		qChapterContent: markRaw(qChapterContent),
		chapterContent: qChapterContent.data,
		setContentId,
		setChapterHref,

		visibleChapters,
		hasHiddenChapters,
		showHiddenChapters,
		setShowHidden,
	}
})

if (import.meta.hot) {
	import.meta.hot.accept(acceptHMRUpdate(useBookDisplayStore, import.meta.hot))
}
