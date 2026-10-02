type OverlapRelation = 'equal' | 'inside' | 'contains'

export interface LabeledPath {
    path: string
    label: string
    libraryId?: string
    /** Nested sources within one library are fine; only exact duplicates overlap. */
    sameLibrary?: boolean
}

interface Overlap extends LabeledPath {
    relation: OverlapRelation
}

/**
 * How `a` relates to `b`, both absolute and resolved. Compares whole segments, so `/books` and
 * `/bookshelf` don't overlap. Case-sensitive, like the filesystems the server usually runs on.
 */
export function overlapRelation(a: string, b: string): OverlapRelation | null {
    const as = a.split('/').filter(Boolean)
    const bs = b.split('/').filter(Boolean)
    for (let i = 0; i < Math.min(as.length, bs.length); i++) {
        if (as[i] !== bs[i]) return null
    }
    if (as.length === bs.length) return 'equal'
    return as.length > bs.length ? 'inside' : 'contains'
}

export function findOverlaps(candidate: string, others: LabeledPath[]): Overlap[] {
    return others.flatMap(o => {
        const relation = overlapRelation(candidate, o.path)
        if (!relation || (o.sameLibrary && relation !== 'equal')) return []
        return [{ ...o, relation }]
    })
}

/** "A", "A and B", "A, B and C". */
function joinList(items: string[]): string {
    return items.length < 2
        ? (items[0] ?? '')
        : `${items.slice(0, -1).join(', ')} and ${items.at(-1)}`
}

/** `short` fits a badge; `long` has one sentence per relation. */
export interface OverlapWarning {
    short: string
    long: string
}

export function describeOverlaps(overlaps: Overlap[]): OverlapWarning | undefined {
    const short: string[] = []
    const long: string[] = []
    for (const relation of ['equal', 'inside', 'contains'] as const) {
        const group = overlaps.filter(o => o.relation === relation)
        if (!group.length) continue
        const labels = joinList([
            ...new Map(group.map(o => [o.libraryId ?? o.label, o.label])).values(),
        ])
        if (relation === 'equal') {
            short.push(`In use by ${labels}`)
            long.push(`Already used by ${labels}.`)
        } else {
            const verb = relation === 'inside' ? 'Inside' : 'Contains'
            const folder = new Set(group.map(o => o.path)).size === 1 ? 'a folder' : 'folders'
            short.push(`${verb} ${labels}`)
            long.push(`${verb} ${folder} used by ${labels}.`)
        }
    }
    return short.length ? { short: short.join(' · '), long: long.join(' ') } : undefined
}
