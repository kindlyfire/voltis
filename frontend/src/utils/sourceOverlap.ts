type OverlapRelation = 'equal' | 'inside' | 'contains'

export interface LabeledPath {
    path: string
    label: string
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
        return relation ? [{ ...o, relation }] : []
    })
}

/** `short` fits a badge, `long` names the paths. */
export function describeOverlap(o: Overlap): { short: string; long: string } {
    switch (o.relation) {
        case 'equal':
            return { short: `In use by ${o.label}`, long: `Already used by ${o.label}` }
        case 'inside':
            return { short: `Inside ${o.label}`, long: `Inside ${o.path}, used by ${o.label}` }
        case 'contains':
            return { short: `Contains ${o.label}`, long: `Contains ${o.path}, used by ${o.label}` }
    }
}
