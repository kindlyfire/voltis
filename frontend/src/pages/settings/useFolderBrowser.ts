import { computed, ref, shallowRef, watch } from 'vue'
import { fsApi } from '@/utils/api/fs'
import type { FolderListing } from '@/utils/api/types'
import { RequestError } from '@/utils/fetch'

const trimSlashes = (path: string) => path.replace(/\/+$/, '') || '/'

/**
 * The folder picker's navigation. `requested` is where the user asked to go (`null` is the home
 * screen), `displayed` the last listing that loaded and `draft` the path bar's text, so a late or
 * failed request never changes the folder "Use this folder" picks.
 */
export function useFolderBrowser() {
    const requested = ref<string | null>(null)
    const hidden = ref(false)
    const list = fsApi.useList(requested, hidden)
    const shown = shallowRef<{ requested: string; listing: FolderListing } | null>(null)
    const opening = ref(false)
    const notice = ref<string>()
    const draft = ref('')
    const editing = ref(false)
    // Bumped on each navigation, so the initial resolve can't override the user's first move.
    let generation = 0

    // The query only exposes its current key's data, so late responses for other paths are dropped.
    watch(list.data, listing => {
        if (listing && requested.value) shown.value = { requested: requested.value, listing }
    })

    const displayed = computed(() => shown.value?.listing ?? null)
    const loading = computed(
        () => opening.value || (requested.value !== null && list.isPending.value)
    )
    const error = computed(() =>
        requested.value !== null && list.isError.value
            ? RequestError.getMessage(list.error.value)
            : null
    )
    const target = computed(() => (loading.value ? null : (displayed.value?.path ?? null)))
    /** Set when the displayed folder was reached through a symlink (or `..`). */
    const resolvedTo = computed(() =>
        shown.value && shown.value.requested !== shown.value.listing.path
            ? shown.value.listing.path
            : null
    )

    /**
     * Falls back to the requested path's parent when nothing loaded, so a failed open isn't a dead
     * end. A typed relative path has no parent: the server rejects it.
     */
    const parent = computed(() => {
        if (displayed.value) return displayed.value.parent
        const path = requested.value
        return path?.startsWith('/') && path !== '/'
            ? path.slice(0, path.lastIndexOf('/')) || '/'
            : null
    })

    function go(path: string | null) {
        generation++
        opening.value = false
        notice.value = undefined
        editing.value = false
        requested.value = path
    }

    // The same path again is a retry: its query key doesn't change.
    function navigate(path: string) {
        const next = trimSlashes(path)
        if (next === requested.value) list.refetch()
        go(next)
    }

    function goHome() {
        go(null)
        shown.value = null
    }

    /** Opens at `path`, or at its nearest existing ancestor if it's gone. */
    async function open(path: string) {
        const gen = ++generation
        opening.value = true
        const [result] = await fsApi.resolve([path], { nearestExisting: true }).catch(() => [])
        if (gen !== generation) return
        navigate(result?.path ?? path)
        if (result?.fallback_from) {
            notice.value = `${result.fallback_from} no longer exists; showing ${result.path}`
        }
    }

    // After a failed navigation, edit what was asked for rather than what's still shown.
    function startEditing() {
        draft.value =
            (error.value ? requested.value : (displayed.value?.path ?? requested.value)) ?? ''
        editing.value = true
    }

    /** Returns whether it navigated; resubmitting the loaded folder just leaves the path bar. */
    function submitDraft() {
        const path = draft.value.trim()
        if (path && (error.value || trimSlashes(path) !== target.value)) {
            navigate(path)
            return true
        }
        editing.value = false
        return false
    }

    return {
        requested,
        hidden,
        shown,
        displayed,
        loading,
        error,
        target,
        resolvedTo,
        parent,
        notice,
        draft,
        editing,
        errorUpdatedAt: list.errorUpdatedAt,
        refetch: list.refetch,
        navigate,
        goHome,
        open,
        startEditing,
        submitDraft,
    }
}
