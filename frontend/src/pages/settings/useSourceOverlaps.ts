import { computed, reactive, watch } from 'vue'
import { fsApi } from '@/utils/api/fs'
import { librariesApi } from '@/utils/api/libraries'
import type { ResolvedPath } from '@/utils/api/types'
import { RequestError } from '@/utils/fetch'
import {
    describeOverlaps,
    findOverlaps,
    type LabeledPath,
    type OverlapWarning,
} from '@/utils/sourceOverlap'

/**
 * Compares source paths with each other and with the other libraries' sources. The server
 * resolves them first (symlinks, relative legacy sources); results are kept for the modal's life.
 */
export function useSourceOverlaps(excludeLibraryId: string | undefined) {
    const libraries = librariesApi.useList()
    const cache = reactive(new Map<string, ResolvedPath>())
    const inFlight = new Set<string>()
    // Paths whose request failed (a timeout, the network): shown, but retried on the next resolve.
    const failed = new Set<string>()

    async function resolve(paths: string[]) {
        const missing = [...new Set(paths)].filter(
            p => p && (!cache.has(p) || failed.has(p)) && !inFlight.has(p)
        )
        for (let i = 0; i < missing.length; i += 100) {
            const chunk = missing.slice(i, i + 100)
            chunk.forEach(p => inFlight.add(p))
            try {
                for (const r of await fsApi.resolve(chunk)) {
                    failed.delete(r.input)
                    cache.set(r.input, r)
                }
            } catch (e) {
                const error = RequestError.getMessage(e)
                for (const p of chunk) {
                    failed.add(p)
                    cache.set(p, { input: p, path: null, error })
                }
            } finally {
                chunk.forEach(p => inFlight.delete(p))
            }
        }
    }

    /** Every library's sources, labelled with the library's name. */
    const allSources = computed(() =>
        (libraries.data.value ?? []).flatMap(l =>
            l.sources.map(s => ({ path: s.path_uri, label: l.name, libraryId: l.id }))
        )
    )
    const otherLibraries = computed(() =>
        allSources.value.filter(s => s.libraryId !== excludeLibraryId)
    )
    watch(otherLibraries, sources => resolve(sources.map(s => s.path)), { immediate: true })

    /**
     * Overlaps of the resolved `path` with other libraries and with `others`, this library's own
     * sources as raw paths, compared once `resolve`d.
     */
    function warning(path: string, others: LabeledPath[]): OverlapWarning | undefined {
        const known = [
            ...otherLibraries.value,
            ...others.map(o => ({ ...o, sameLibrary: true })),
        ].flatMap(o => {
            const real = cache.get(o.path)?.path
            return real ? [{ ...o, path: real }] : []
        })
        return describeOverlaps(findOverlaps(path, known))
    }

    /** Like `warning`, for a raw path; nothing until it's `resolve`d. */
    function rawWarning(raw: string, others: LabeledPath[]): OverlapWarning | undefined {
        const r = cache.get(raw)
        if (!r) return undefined
        if (!r.path)
            return { short: 'Overlap unknown', long: `Could not check overlap: ${r.error}` }
        return warning(r.path, others)
    }

    return {
        allSources,
        resolve,
        retry: () => resolve([...failed]),
        resolved: (raw: string) => cache.get(raw),
        warning,
        rawWarning,
    }
}
