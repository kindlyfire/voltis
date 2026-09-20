export interface EpubTarget {
    href: string
    fragment: string
}

const SCHEME_RE = /^[a-z][a-z0-9+.-]*:/i
const ENCODED_SEPARATOR_RE = /%2f|%5c/i

export function isExternalRef(ref: string): boolean {
    return SCHEME_RE.test(ref) || ref.startsWith('//')
}

function decodeOnce(part: string): string {
    try {
        return decodeURIComponent(part)
    } catch {
        return part
    }
}

export function dirOf(documentHref: string): string {
    const i = documentHref.lastIndexOf('/')
    return i === -1 ? '' : documentHref.slice(0, i)
}

function isValidFileReference(name: string): boolean {
    if (!name || name.includes('\\') || name.includes('\0') || name.startsWith('/')) return false
    const last = name.slice(name.lastIndexOf('/') + 1)
    return last !== '' && last !== '.' && last !== '..'
}

function joinAndClean(baseDir: string, ref: string): string | null {
    const out: string[] = []
    for (const part of `${baseDir}/${ref}`.split('/')) {
        if (part === '' || part === '.') continue
        if (part === '..') {
            if (out.length && out[out.length - 1] !== '..') out.pop()
            else out.push('..')
            continue
        }
        out.push(part)
    }
    const clean = out.join('/')
    if (!clean || clean === '..' || clean.startsWith('../')) return null
    return clean
}

/** Mirrors the backend's `resolveTarget`: same base-is-a-document rule, same
 * rejections, so a reference that works as a TOC target also works as a link. */
export function resolveEpubRef(baseDocument: string, ref: string): EpubTarget | null {
    const trimmed = ref.trim()
    if (!trimmed) return null

    const hash = trimmed.indexOf('#')
    const beforeHash = hash === -1 ? trimmed : trimmed.slice(0, hash)
    const fragment = decodeOnce(hash === -1 ? '' : trimmed.slice(hash + 1))
    const query = beforeHash.indexOf('?')
    const rawPath = query === -1 ? beforeHash : beforeHash.slice(0, query)

    if (isExternalRef(rawPath) || ENCODED_SEPARATOR_RE.test(rawPath)) return null

    const decoded = decodeOnce(rawPath)
    if (!decoded) return baseDocument ? { href: baseDocument, fragment } : null
    if (!isValidFileReference(decoded)) return null

    const href = joinAndClean(dirOf(baseDocument), decoded)
    return href && isValidFileReference(href) ? { href, fragment } : null
}
