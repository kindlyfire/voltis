import { acceptHMRUpdate, defineStore } from 'pinia'
import { ref, type Ref } from 'vue'
import { useRouter, type Router } from 'vue-router'
import { isBookLocator, type BookEntry } from './bookEntry'
import { useBookSettings } from './bookSettings'
import {
    createBookSession,
    type BookAnchor,
    type BookNav,
    type BookSession,
} from './createBookSession'

function anchorQuery(target: BookAnchor) {
    return target.fragment ? { ch: target.href, frag: target.fragment } : { ch: target.href }
}

/** History snapshots are scoped to the book, so an entry stamped by the
 * previous one can never be restored onto this one. */
export function createBookNav(router: Router, contentId: string): BookNav {
    return {
        push(target) {
            router.push({ query: anchorQuery(target) })
        },
        replace(target) {
            router.replace({ query: anchorQuery(target) })
        },
        saveLocator(locator) {
            history.replaceState(
                { ...(history.state ?? {}), bookLocator: { contentId, locator } },
                ''
            )
        },
        historyLocator() {
            const saved = (history.state as Record<string, any> | null)?.bookLocator
            if (!saved || saved.contentId !== contentId) return null
            return isBookLocator(saved.locator) ? saved.locator : null
        },
    }
}

export const useBookDisplayStore = defineStore('book-display', () => {
    const router = useRouter()
    const sidebarOpen = ref(false)
    const session: Ref<BookSession | null> = ref(null)
    const settings = useBookSettings()

    function setContent(contentId: string, entry: BookEntry) {
        if (session.value?.contentId === contentId) {
            session.value.setEntry(entry)
            return
        }
        session.value?.dispose()
        session.value = createBookSession(contentId, entry, createBookNav(router, contentId))
    }

    function dispose() {
        sidebarOpen.value = false
        const current = session.value
        session.value = null
        return current?.dispose()
    }

    return { session, sidebarOpen, settings, setContent, dispose }
})

if (import.meta.hot) {
    import.meta.hot.accept(acceptHMRUpdate(useBookDisplayStore, import.meta.hot))
}
