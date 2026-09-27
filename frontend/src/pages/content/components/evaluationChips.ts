import type { Evaluation } from '@/utils/api/metadata'

/** How a candidate matches the series, as chips. */
export function evaluationChips(e: Evaluation) {
    const chips: { label: string; tone: 'success' | 'danger' | 'neutral' }[] = [
        e.exact
            ? { label: 'Exact title', tone: 'success' }
            : { label: `Title ${Math.round(e.title * 100)}%`, tone: 'neutral' },
    ]
    if (e.year)
        chips.push({
            label: `Year ${e.year}`,
            tone: e.year === 'match' ? 'success' : 'danger',
        })
    if (e.staff) {
        chips.push({
            label: `Staff ${e.staff}`,
            tone: e.staff === 'match' ? 'success' : 'danger',
        })
    }
    if (e.volumes) chips.push({ label: 'More volumes than upstream', tone: 'danger' })
    return chips
}
