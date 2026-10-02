import { vi } from 'vitest'
import { ReadingConflict, readingApi } from '@/utils/api/reading'
import type {
    ReadingRequest,
    ReadingResponse,
    ReadingState,
    ReadingStateResponse,
    ReadingStatus,
    SeriesPrevious,
} from '@/utils/api/types'

export type Sent = ReadingRequest & { base_revision: string | null; writer_id: string; seq: number }

/** For tests: one user's reading rows behind a mocked `readingApi`, applying the transition rules
 * and the revision contract, with controls for holding, failing and losing requests. Items end on
 * page `last`. */
export function createReadingServer(last: number) {
    const rows = new Map<string, ReadingState>()
    const parents = new Map<string, string>()
    let n = 0
    const blank = (): ReadingState => ({
        revision: null,
        status: null,
        status_updated_at: null,
        progress: {},
        progress_updated_at: null,
        last_read_at: null,
    })
    const stateOf = (id: string) => rows.get(id) ?? blank()
    const writerOf = (rev: string | null) => (rev?.startsWith('t') ? rev.split(':')[0]! : null)
    const isHeld = (s: ReadingStatus | null) =>
        s === 'plan_to_read' || s === 'on_hold' || s === 'dropped'

    function env(id: string): ReadingStateResponse {
        const parent = parents.get(id)
        const kids = [...parents].filter(([, p]) => p === parent).map(([k]) => stateOf(k).status)
        const series = parent && {
            id: parent,
            status: stateOf(parent).status,
            revision: stateOf(parent).revision,
            children_count: kids.length,
            completed_children_count: kids.filter(s => s === 'completed').length,
            dropped_children_count: kids.filter(s => s === 'dropped').length,
            caught_up: kids.every(s => s === 'completed' || s === 'dropped'),
        }
        const state = stateOf(id)
        return structuredClone({ state, series: series || null, writer: writerOf(state.revision) })
    }

    function set(id: string, patch: Partial<ReadingState>, rev = `srv:${++n}`) {
        rows.set(id, { ...stateOf(id), ...patch, revision: rev })
    }

    function startSeries(id: string, rev: string, reopen = false) {
        const parent = parents.get(id)
        if (!parent) return null
        const { status, status_updated_at } = stateOf(parent)
        if (status !== null && status !== 'plan_to_read' && !(reopen && status === 'completed')) {
            return null
        }
        set(parent, { status: 'reading', status_updated_at: 'now' }, rev)
        return { status, status_updated_at, revision: rev }
    }

    const revertedSeries = (s: SeriesPrevious) => ({
        status: s.status,
        status_updated_at: s.status_updated_at,
    })

    function apply(id: string, req: Sent): ReadingResponse {
        const cur = stateOf(id)
        const fenced = req.op === 'series_status' ? stateOf(parents.get(id)!) : cur
        const [w, s] = fenced.revision?.split(':') ?? []
        const res = (outcome: ReadingResponse['outcome'], over: Partial<ReadingResponse> = {}) => ({
            ...env(id),
            outcome,
            previous: null,
            series_previous: null,
            ...over,
        })
        if (w === req.writer_id && req.seq <= Number(s)) return res('none')
        if (req.op !== 'series_status' && cur.revision !== req.base_revision) {
            throw new ReadingConflict(env(id))
        }
        const rev = `${req.writer_id}:${req.seq}`
        const end = { current_page: last, progress_percent: 100, at_end: true }
        const prev = { status: cur.status, progress: cur.progress, last_read_at: cur.last_read_at }
        switch (req.op) {
            case 'position': {
                const { at_end: _, ...progress } = req.progress
                const status = cur.status === 'completed' ? 'completed' : 'reading'
                set(id, { status, progress, last_read_at: 'now' }, rev)
                const outcome = !cur.status
                    ? 'started'
                    : isHeld(cur.status)
                      ? 'moved_to_reading'
                      : 'saved'
                const series_previous = outcome === 'saved' ? null : startSeries(id, rev, true)
                return res(outcome, { previous: isHeld(cur.status) ? prev : null, series_previous })
            }
            case 'finish': {
                if (cur.status === 'completed') return res('none')
                set(id, { status: 'completed', progress: end, last_read_at: 'now' }, rev)
                const series_previous = startSeries(id, rev)
                return res('completed', {
                    previous: isHeld(cur.status) ? prev : null,
                    series_previous,
                })
            }
            case 'mark_completed':
                set(id, { status: 'completed', progress: end }, rev)
                return res('completed', { series_previous: startSeries(id, rev) })
            case 'set_status':
                set(id, { status: req.status, status_updated_at: 'now' }, rev)
                return res('status_set')
            case 'clear':
                set(id, blank(), rev)
                return res('cleared')
            case 'restore': {
                set(id, req.snapshot, rev)
                const parent = parents.get(id)
                if (req.series && parent && stateOf(parent).revision === req.series.revision) {
                    set(parent, revertedSeries(req.series), rev)
                }
                return res('restored')
            }
            case 'series_status': {
                const parent = parents.get(id)!
                if (!('series' in req)) set(parent, { status: req.status }, rev)
                else if (stateOf(parent).revision === req.series.revision) {
                    set(parent, revertedSeries(req.series), rev)
                }
                return res('series_status')
            }
        }
    }

    const server = {
        rows,
        parents,
        stateOf,
        set,
        /** Clears the item as another device's clear does. */
        clear: (id: string) => set(id, blank()),
        offline: false,
        /** Requests that fail as if offline. */
        refuse: (_req: Sent | 'GET') => false,
        /** The next write lands, but its response is lost. */
        loseAck: false,
        holding: false,
        held: [] as (() => void)[],
        inFlight: 0,
        maxInFlight: 0,
        sent: [] as Array<{ id: string; req: Sent | 'GET'; keepalive: boolean }>,
        release() {
            server.holding = false
            server.held.splice(0).forEach(r => r())
        },
    }

    async function call<T>(
        id: string,
        req: Sent | 'GET',
        init: RequestInit | undefined,
        fn: () => T
    ) {
        server.sent.push({ id, req, keepalive: !!init?.keepalive })
        server.maxInFlight = Math.max(server.maxInFlight, ++server.inFlight)
        try {
            if (server.holding) await new Promise<void>(r => server.held.push(r))
            if (server.offline || server.refuse(req)) throw new TypeError('Failed to fetch')
            const result = fn()
            if (req !== 'GET' && server.loseAck) {
                server.loseAck = false
                throw new TypeError('Failed to fetch')
            }
            return result
        } finally {
            server.inFlight--
        }
    }
    vi.mocked(readingApi.get).mockImplementation(id => call(id, 'GET', undefined, () => env(id)))
    vi.mocked(readingApi.seriesReading).mockImplementation((seriesId, body) =>
        call(seriesId, 'GET', undefined, () => {
            const b = body as Extract<typeof body, { action: 'mark_series_completed' }>
            const rev = `${b.writer_id}:${b.seq}`
            const kids = [...parents].filter(([, p]) => p === seriesId).map(([k]) => k)
            for (const kid of kids) {
                const st = stateOf(kid).status
                if (b.include_unread && st !== 'completed' && st !== 'dropped') {
                    set(
                        kid,
                        { status: 'completed', progress: { current_page: last, at_end: true } },
                        rev
                    )
                }
            }
            set(seriesId, { status: 'completed' }, rev)
            return { count: kids.length }
        })
    )
    vi.mocked(readingApi.post).mockImplementation((id, req, init) =>
        call(id, req as Sent, init, () => apply(id, req as Sent))
    )
    return server
}
