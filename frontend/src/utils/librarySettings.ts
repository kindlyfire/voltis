import type { Library, LibrarySettings, LibrarySource, SourceSettings } from './api/types'

/** The settings of a file under the source: the source's keys over the library's (`Resolve`). */
export function resolveSettings(lib: LibrarySettings, src: SourceSettings): LibrarySettings {
    return { ...lib, auto_match: { ...lib.auto_match, ...src.auto_match } }
}

export function autoMatchOn(lib: LibrarySettings, src: SourceSettings, provider: string) {
    return !!resolveSettings(lib, src).auto_match[provider]
}

/** Whether the server attributes files to the source: a path that cleans to "." has no prefix. */
function hasPrefix(path: string) {
    if (path.startsWith('/')) return true
    const parts: string[] = []
    for (const p of path.split('/')) {
        if (p === '..' && parts.length && parts.at(-1) !== '..') parts.pop()
        else if (p && p !== '.') parts.push(p)
    }
    return parts.length > 0
}

/** Whether the library matches some series automatically with one of the providers (`any()`). */
export function libraryAutoMatches(library: Library, providers: string[]) {
    return providers.some(
        p =>
            !!library.settings.auto_match[p] ||
            library.sources.some(s => hasPrefix(s.path_uri) && s.settings.auto_match?.[p] === true)
    )
}

/** How many sources override the provider's automatic matching. */
export function overrideCount(sources: Pick<LibrarySource, 'settings'>[], provider: string) {
    return sources.filter(s => s.settings.auto_match?.[provider] !== undefined).length
}
