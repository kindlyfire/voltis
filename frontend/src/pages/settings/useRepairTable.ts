import { refDebounced } from '@vueuse/core'
import { computed, reactive, ref, watch, type Ref } from 'vue'
import type { PageParams } from '@/utils/api/content'
import { librariesApi } from '@/utils/api/libraries'

const PAGE_SIZE = 50

/**
 * State for a per-library, paginated table of rows keyed by id, each marked for deletion or
 * pointed at a content URI, all saved at once.
 */
export function useRepairTable<Q extends { data: Readonly<Ref<{ total: number } | undefined>> }>(
    summary: () => { library_id: string | null; count: number }[] | undefined,
    list: (libraryId: Ref<string | null>, params: () => PageParams) => Q
) {
    const qLibraries = librariesApi.useList()
    const selectedLibraryId = ref<string | null>(null)
    const searchInput = ref('')
    const search = refDebounced(searchInput, 300)
    const page = ref(1)
    const edits = reactive(new Map<string, string | 'delete'>())

    const libraryOptions = computed(() =>
        // Rows whose library was deleted have no library to fix them in.
        (summary() ?? []).flatMap(s => {
            if (!s.library_id) return []
            const lib = qLibraries.data.value?.find(l => l.id === s.library_id)
            return [{ value: s.library_id, label: `${lib?.name ?? s.library_id} (${s.count})` }]
        })
    )

    watch(
        libraryOptions,
        options => {
            if (!options.some(o => o.value === selectedLibraryId.value)) {
                selectedLibraryId.value = options[0]?.value ?? null
            }
        },
        { immediate: true }
    )

    const query = list(selectedLibraryId, () => ({
        search: search.value || undefined,
        limit: PAGE_SIZE,
        offset: (page.value - 1) * PAGE_SIZE,
    }))
    const pageCount = computed(() => Math.ceil((query.data.value?.total ?? 0) / PAGE_SIZE))

    // Saving can shrink the list below the current page, also when the pagination isn't rendered.
    watch(
        () => query.data.value?.total,
        total => {
            if (total == null) return
            page.value = Math.min(page.value, Math.max(1, Math.ceil(total / PAGE_SIZE)))
        }
    )

    watch([selectedLibraryId, search], () => {
        edits.clear()
        page.value = 1
    })

    return {
        selectedLibraryId,
        libraryOptions,
        searchInput,
        search,
        page,
        pageCount,
        query,
        edits,

        /** The URI a row is pointed at, if any. */
        target(id: string): string | null {
            const edit = edits.get(id)
            return edit && edit !== 'delete' ? edit : null
        },

        setTarget(id: string, uri: string | undefined) {
            if (uri) {
                edits.set(id, uri)
            } else {
                edits.delete(id)
            }
        },

        toggleDelete(id: string) {
            if (edits.get(id) === 'delete') {
                edits.delete(id)
            } else {
                edits.set(id, 'delete')
            }
        },

        /** The edits as the ids to delete and the URIs to point the others at. */
        changes() {
            const deletes: string[] = []
            const targets: Record<string, string> = {}
            for (const [id, value] of edits) {
                if (value === 'delete') {
                    deletes.push(id)
                } else {
                    targets[id] = value
                }
            }
            return { deletes, targets }
        },
    }
}
