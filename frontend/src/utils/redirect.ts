// A fixed base keeps this pure; a value that parses to another origin isn't an in-app path.
const BASE = 'http://voltis.invalid'

// Route matching is case-insensitive, so `/AUTH/login` is the login page too.
const isAuthPath = (path: string) => /^\/auth(\/|$)/i.test(path)

/** The in-app path to return to after signing in, or `null` if `value` isn't a safe one. */
export function safeRedirect(value: unknown): string | null {
    if (typeof value !== 'string' || value.length > 2048) return null
    if (!value.startsWith('/') || value.startsWith('//')) return null
    // Browsers strip tabs and newlines and treat `\` as `/`, so `/\t/evil` would become `//evil`.
    if (/[\x00-\x1f\x7f\\]/.test(value)) return null
    const u = new URL(value, BASE)
    // Dot segments are resolved, so `/..//evil` comes out as `//evil`.
    if (u.origin !== BASE || u.pathname.startsWith('//') || isAuthPath(u.pathname)) return null
    return u.pathname + u.search + u.hash
}

/** `{ redirect }` for a router link's query when `value` is a safe redirect other than `/`. */
export function redirectQuery(value: unknown): { redirect?: string } {
    const v = safeRedirect(value)
    return v && v !== '/' ? { redirect: v } : {}
}
