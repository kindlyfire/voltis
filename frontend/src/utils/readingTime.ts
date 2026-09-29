import type { ContentLength, UserPreferences } from './api/types'

export const DEFAULT_WORDS_PER_MINUTE = 250
export const DEFAULT_SECONDS_PER_PAGE = 20

interface ReadingSpeed {
    wordsPerMinute: number
    secondsPerPage: number
}

export function readingSpeed(prefs?: UserPreferences): ReadingSpeed {
    return {
        wordsPerMinute: prefs?.reading?.wordsPerMinute ?? DEFAULT_WORDS_PER_MINUTE,
        secondsPerPage: prefs?.reading?.secondsPerPage ?? DEFAULT_SECONDS_PER_PAGE,
    }
}

/** Coarser the longer it gets. Buckets go by the rounded value, so 59.7 min is `1 h`. */
export function formatDuration(minutes: number): string {
    if (minutes < 1) return '< 1 min'
    const rounded = Math.round(minutes)
    if (rounded < 60) return `${rounded} min`
    const fives = Math.round(minutes / 5) * 5
    if (fives < 600) {
        const h = Math.floor(fives / 60)
        const m = fives % 60
        return m ? `${h} h ${m} min` : `${h} h`
    }
    return `${Math.round(minutes / 60)} h`
}

const compact = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 })

export function lengthSummary(length: ContentLength, speed: ReadingSpeed): string {
    const { unit, total, remaining } = length
    const count =
        unit === 'words'
            ? `${compact.format(total)} words`
            : `${total.toLocaleString('en')} ${total === 1 ? 'page' : 'pages'}`
    const minutes = (n: number) =>
        unit === 'words' ? n / speed.wordsPerMinute : (n * speed.secondsPerPage) / 60
    if (remaining === 0 || remaining === total) {
        return `${count} · ${formatDuration(minutes(total))}`
    }
    return `${count} · ${formatDuration(minutes(remaining))} left`
}
