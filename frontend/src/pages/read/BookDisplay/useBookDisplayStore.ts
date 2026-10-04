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
            return router.push({ query: anchorQuery(target) })
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
        beforeLeave(callback) {
            return router.beforeEach((_to, from) => {
                // Back and Forward have already moved the entry: stamping now
                // would write onto the destination. `current` is vue-router's
                // own HTML5 history state (not public API); without it this
                // never stamps and the history locator lags by up to 500 ms.
                if ((history.state as Record<string, any> | null)?.current === from.fullPath) {
                    callback()
                }
            })
        },
        historyLocator() {
            const saved = (history.state as Record<string, any> | null)?.bookLocator
            if (!saved || saved.contentId !== contentId) return null
            return isBookLocator(saved.locator) ? saved.locator : null
        },
    }
}

export type DrawerTab = 'contents' | 'settings'

export const useBookDisplayStore = defineStore('book-display', () => {
    const router = useRouter()
    const sidebarOpen = ref(false)
    const session: Ref<BookSession | null> = ref(null)
    const settings = useBookSettings()
    /** The drawer's open tab and the settings tab's scroll, kept while reading. */
    const drawerTab = ref<DrawerTab>('contents')
    const settingsScroll = ref<number | null>(null)

    function setContent(contentId: string, entry: BookEntry) {
        if (session.value?.contentId === contentId) {
            session.value.setEntry(entry)
            return
        }
        // The next session's load queues behind this one's exit write.
        session.value?.dispose()
        session.value = createBookSession(
            contentId,
            entry,
            createBookNav(router, contentId),
            settings
        )
    }

    function dispose() {
        sidebarOpen.value = false
        drawerTab.value = 'contents'
        settingsScroll.value = null
        session.value?.dispose()
        session.value = null
    }

    return {
        session,
        sidebarOpen,
        drawerTab,
        settingsScroll,
        settings,
        setContent,
        dispose,
    }
})

if (import.meta.hot) {
    import.meta.hot.accept(acceptHMRUpdate(useBookDisplayStore, import.meta.hot))
}
