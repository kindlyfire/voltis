export function formatBytes(bytes: number | null | undefined): string {
    if (!bytes) return '—'
    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    let size = bytes
    let i = 0
    while (size >= 1024 && i < units.length - 1) {
        size /= 1024
        i++
    }
    return `${size.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

const relative = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto', style: 'short' })
const RELATIVE_UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
    ['year', 31536000],
    ['month', 2592000],
    ['day', 86400],
    ['hour', 3600],
    ['minute', 60],
]

/** "3 hr. ago" for a timestamp in the past. */
export function formatRelative(ts: string, now = Date.now()): string {
    const seconds = Math.round((Date.parse(ts) - now) / 1000)
    for (const [unit, size] of RELATIVE_UNITS) {
        if (Math.abs(seconds) >= size) return relative.format(Math.round(seconds / size), unit)
    }
    return relative.format(0, 'second')
}
