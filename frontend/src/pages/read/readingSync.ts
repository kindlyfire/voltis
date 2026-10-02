import { computed, reactive, shallowReactive } from 'vue'
import { useToast } from '@/ui/useToast'
import {
    bumpContinueReading,
    invalidateReading,
    markPositionSaved,
    ReadingConflict,
    readingApi,
} from '@/utils/api/reading'
import {
    READING_STATUS_LABELS,
    type Content,
    type ReadingProgress,
    type ReadingRequest,
    type ReadingResponse,
    type ReadingSnapshot,
    type ReadingState,
    type ReadingStateResponse,
    type ReadingStatus,
    type SeriesPrevious,
    type SeriesReadingInfo,
} from '@/utils/api/types'
import { RequestError } from '@/utils/fetch'
import { queryClient } from '@/utils/misc'
import { Modals } from '@/utils/modals'
import { showClearReadingModal } from '../content/components/ClearReadingModal.vue'
import { showMarkSeriesCompletedModal } from '../content/components/MarkSeriesCompletedModal.vue'
import ReadingConflictModal, { type ConflictChoice } from './ReadingConflictModal.vue'
import SeriesHeldModal, { type SeriesHeldChoice } from './SeriesHeldModal.vue'

const WRITE_DEBOUNCE = 1000

export interface ReaderAdapter {
    content(): Content | null
    /** Places the reader at a saved position. Placement writes nothing. */
    restore(progress: ReadingProgress): void
    /** "p.54", "54 %". */
    describe(progress: ReadingProgress): string
    /** Close enough for reconciling with another device: no jump, no conflict. */
    samePosition(a: ReadingProgress, b: ReadingProgress): boolean
    /** Exactly the same place (locator, page): a report there trails a finish taken there. */
    samePlace(a: ReadingProgress, b: ReadingProgress): boolean
}

type Command = Extract<
    ReadingRequest,
    { op: 'set_status' | 'mark_completed' | 'clear' | 'series_status' }
>

/** An item's reading as this page knows it, whether a reader shows it or not. */
interface Lane {
    id: string
    adapter: ReaderAdapter | null
    /** The content when its reader detached, for feedback and cache keys. */
    content: Pick<Content, 'title' | 'parent_id'> | null
    acked: ReadingState | null
    series: SeriesReadingInfo | null
    here: ReadingProgress | null
    tracking: boolean
    /** The reader has placed itself: placements now go to it, not to where it opens. */
    ready: boolean
    /** Reading waits for new reading or Retry after a failed send, */
    failed: boolean
    /** and for a check, or the answer to a conflict. */
    held: boolean
    /** A conflict waits to be asked about. */
    stale: boolean
    failures: number
    /** Saved positions not yet fully refetched. */
    dirty: boolean
}

interface Receipt {
    lane: Lane
    previous: ReadingSnapshot
    series: SeriesPrevious | null
}

type Deferred<T = void> = ReturnType<typeof Promise.withResolvers<T>>

type Job =
    /** `superseded`: replaced while out, it is never sent again. */
    | {
          kind: 'position'
          lane: Lane
          progress: ReadingProgress
          sealed: boolean
          superseded?: boolean
      }
    | { kind: 'finish'; lane: Lane; progress: ReadingProgress; superseded?: boolean }
    | { kind: 'command'; lane: Lane; req: Command; done: Deferred; after: string[] }
    | {
          kind: 'series'
          lane: Lane
          seriesId: string
          includeUnread: boolean
          done: Deferred
          after: string[]
      }
    | { kind: 'undo'; lane: Lane; receipt: number }
    /** A check, or with `done` the reader's initial read. */
    | { kind: 'read'; lane: Lane; done?: Deferred<ReadingProgress> }

const isEmpty = (p: ReadingProgress) => Object.keys(p).length === 0
/** Cleared, as a clear leaves it: a status cleared on its own keeps its time. */
const isCleared = (s: ReadingState) =>
    s.status === null && isEmpty(s.progress) && !s.status_updated_at && !s.last_read_at
const isReading = (job: Job) => job.kind === 'position' || job.kind === 'finish'
const isHeld = (s: ReadingStatus | null): s is 'on_hold' | 'dropped' =>
    s === 'on_hold' || s === 'dropped'
const signedOut = () => new Error('Signed in as someone else')

const currentUserId = () =>
    queryClient.getQueryData<{ id: string } | null>(['users', 'me'])?.id ?? ''

/**
 * The page's one link to the server's reading state, surviving reader navigation. Requests go out
 * one at a time, so no response overtakes another, and every response is adopted or compared the
 * same way. A reader takes part only once its positions can be compared (`load()`); until then,
 * what comes in for its item waits for that load to reconcile it.
 */
export function createReadingActor() {
    const writerId =
        't' +
        Array.from(crypto.getRandomValues(new Uint8Array(16)), b => (b % 36).toString(36)).join('')
    let seq = 0
    const lanes = new Map<string, Lane>()
    const queue: Job[] = []
    const receipts = new Map<number, Receipt>()
    let nextReceipt = 0
    let current: Job | null = null
    let dialog: Promise<void> | null = null
    let frozen = false
    let keepalive = false
    let timer: ReturnType<typeof setTimeout> | null = null
    const shown = new Set<string>()
    const askedSeries = new Set<string>()

    // Everything here belongs to the account that started it. Signing out stops it; the next
    // account to sign in, even the same one, gets a fresh page.
    let owner = currentUserId()
    const unsubscribe = queryClient.getQueryCache().subscribe(e => {
        if (e.query.queryKey[0] !== 'users' || e.query.queryKey[1] !== 'me') return
        const me = e.query.state.data as { id: string } | null | undefined
        if (me === undefined) return
        owner ||= me?.id ?? ''
        if (me?.id === owner && !frozen) return
        freeze()
        if (me) window.location.reload()
    })

    function freeze() {
        frozen = true
        if (timer) clearTimeout(timer)
        // The job out is abandoned now too; its response is ignored whenever it comes.
        for (const job of [current, ...queue.splice(0)]) if (job) abandon(job)
    }

    function abandon(job: Job) {
        if ('done' in job) job.done?.reject(signedOut())
    }

    function laneOf(id: string) {
        let lane = lanes.get(id)
        if (!lane) {
            // Shallow: the progress handed to readers has to stay cloneable, for history state.
            lane = shallowReactive<Lane>({
                id,
                adapter: null,
                content: null,
                acked: null,
                series: null,
                here: null,
                tracking: true,
                ready: false,
                failed: false,
                held: false,
                stale: false,
                failures: 0,
                dirty: false,
            }) as Lane
            lanes.set(id, lane)
        }
        return lane
    }

    const contentOf = (lane: Lane) => lane.adapter?.content() ?? lane.content
    const parentOf = (lane: Lane) => contentOf(lane)?.parent_id ?? null
    const hasReading = (lane: Lane) => queue.some(j => j.lane === lane && isReading(j))

    /** Where the item stands finished: at its latest unanswered reading if that is a finish, else
     * at the saved place of a completed item. Reports there trail the finish, and finishing again
     * means nothing. Derived, so whatever drops or answers a finish rearms it. */
    function finishedAt(lane: Lane): ReadingProgress | null {
        let last: Job | null = null
        for (const job of [current, ...queue]) {
            if (job?.lane === lane && isReading(job) && !job.superseded) last = job
        }
        if (last) return last.kind === 'finish' ? last.progress : null
        return lane.acked?.status === 'completed' ? lane.acked.progress : null
    }
    const visible = () => document.visibilityState !== 'hidden'

    /** Commands wait, like reading, for a check or conflict to be answered: none overtakes it. */
    function runnable(job: Job) {
        if (job.kind === 'read' || job.kind === 'undo') return true
        if (job.lane.held) return false
        if (!isReading(job)) return true
        if (job.kind === 'position' && !job.sealed) return false
        return !job.lane.failed
    }

    function pump() {
        if (current || frozen || dialog) return
        const i = queue.findIndex(runnable)
        if (i < 0) return
        const job = (current = queue.splice(i, 1)[0]!)
        const init = keepalive ? { keepalive: true } : undefined
        keepalive = false
        void run(job, init).finally(() => {
            current = null
            pump()
        })
    }

    function push(job: Job) {
        if (job.kind !== 'position') seal()
        queue.push(job)
        pump()
    }

    /** Ends the run of positions: they go as soon as they can, and nothing merges into them. */
    function seal() {
        if (timer) clearTimeout(timer)
        timer = null
        for (const job of queue) if (job.kind === 'position') job.sealed = true
    }

    function sendNow() {
        seal()
        pump()
    }

    function dropReading(lane: Lane) {
        for (let i = queue.length - 1; i >= 0; i--) {
            if (queue[i]!.lane === lane && isReading(queue[i]!)) queue.splice(i, 1)
        }
        if (current?.lane === lane && isReading(current)) current.superseded = true
        lane.failed = false
        lane.failures = 0
    }

    function adopt(lane: Lane, env: ReadingStateResponse) {
        lane.acked = env.state
        lane.series = env.series
    }

    /** Every response's way in: the expected state is adopted, anything else compared. */
    function receive(lane: Lane, env: ReadingStateResponse) {
        lane.series = env.series
        const acked = lane.acked
        if (!acked || env.state.revision === acked.revision || env.writer === writerId) {
            return adopt(lane, env)
        }
        if (!lane.adapter || dialog || !visible()) {
            lane.stale = lane.held = true
            return
        }
        compare(lane, env)
    }

    function body(job: Exclude<Job, { kind: 'read' | 'series' }>): ReadingRequest {
        if (job.kind === 'command') return job.req
        if (job.kind === 'undo') {
            const r = receipts.get(job.receipt)!
            return { op: 'restore', snapshot: r.previous, series: r.series }
        }
        return { op: job.kind, progress: job.progress }
    }

    async function run(job: Job, init?: RequestInit) {
        const { lane } = job
        try {
            if (job.kind === 'read') {
                const env = await readingApi.get(lane.id)
                if (frozen) return abandon(job)
                const retained = !!lane.acked && (hasReading(lane) || lane.stale)
                lane.held = lane.stale = false
                if (!job.done || retained) {
                    receive(lane, env)
                } else {
                    adopt(lane, env)
                    lane.here = env.state.progress
                }
                if (!job.done) return
                // The reader opens where reconciling leaves it.
                await dialog
                if (frozen) return abandon(job)
                lane.ready = true
                return job.done.resolve(lane.here ?? env.state.progress)
            }
            if (job.kind === 'series') {
                await readingApi.seriesReading(job.seriesId, {
                    action: 'mark_series_completed',
                    include_unread: job.includeUnread,
                    writer_id: writerId,
                    seq: ++seq,
                })
                const env = await readingApi.get(lane.id)
                if (frozen) return abandon(job)
                receive(lane, env)
                return job.done.resolve()
            }
            if (job.kind === 'undo' && !receipts.has(job.receipt)) return
            const req = Object.freeze({
                ...body(job),
                base_revision: lane.acked?.revision ?? null,
                writer_id: writerId,
                seq: ++seq,
            })
            const res = await readingApi.post(lane.id, req, init)
            if (frozen) return abandon(job)
            if (isReading(job)) lane.failures = 0
            receive(lane, res)
            if (job.kind === 'command') job.done.resolve()
            if (job.kind === 'undo') receipts.delete(job.receipt)
            if (isReading(job)) feedback(lane, res)
            refetch(lane, res)
        } catch (err) {
            if (frozen) abandon(job)
            else failed(job, err)
        }
    }

    function failed(job: Job, err: unknown) {
        const { lane } = job
        if (job.kind === 'read') {
            lane.held = lane.stale
            if (job.done) job.done.reject(err)
            else console.error('Failed to check the reading state', err)
            return
        }
        const conflicted = err instanceof ReadingConflict
        if (isReading(job) && job.superseded) {
            if (conflicted) receive(lane, err.current)
            return
        }
        if (conflicted && err.current.writer === writerId) {
            // Our own write, whose acknowledgement was lost: this one goes again on top of it.
            adopt(lane, err.current)
            return void queue.unshift(job)
        }
        if (isReading(job)) {
            queue.unshift(job)
            if (!conflicted) {
                console.error('Failed to save reading progress', err)
                // Operations building on it would overtake it: they fail with it.
                for (let i = queue.length - 1; i >= 0; i--) {
                    const later = queue[i]!
                    if (
                        (later.kind === 'command' || later.kind === 'series') &&
                        later.after.includes(lane.id)
                    ) {
                        queue.splice(i, 1)
                        later.done.reject(err)
                    }
                }
                lane.failed = true
                // Reading on retries a position, but nothing retries a finish but Retry: offer it now.
                lane.failures =
                    job.kind === 'finish' ? Math.max(lane.failures + 1, 3) : lane.failures + 1
            }
        } else if (job.kind === 'undo') {
            failure('undo', err, () => undo(job.receipt))
        } else {
            job.done.reject(err)
        }
        if (conflicted) receive(lane, err.current)
    }

    function refetch(lane: Lane, res: ReadingResponse) {
        if (res.outcome === 'none') return
        if (res.outcome !== 'saved' || !lane.adapter) {
            lane.dirty = false
            return void invalidateReading(lane.id, parentOf(lane))
        }
        // While reading, the rest refetches when the reader leaves: not on every page.
        lane.dirty = true
        markPositionSaved(lane.id, parentOf(lane))
    }

    /** Reading that went against a deliberate status gets an Undo; a held series asks once. */
    function feedback(lane: Lane, res: ReadingResponse) {
        if (!['saved', 'started', 'moved_to_reading', 'completed'].includes(res.outcome)) return
        const keep = () => {
            receipts.set(++nextReceipt, {
                lane,
                previous: res.previous!,
                series: res.series_previous,
            })
            return nextReceipt
        }
        const status = res.series?.status ?? null
        if (isHeld(status) && !dialog && !askedSeries.has(res.series!.id)) {
            askedSeries.add(res.series!.id)
            const receipt = res.previous && keep()
            const title = contentOf(lane)?.title || 'This item'
            const item = !receipt
                ? null
                : res.outcome === 'completed'
                  ? `${title} marked completed.`
                  : `${title} moved to Reading.`
            return openDialog(async () => {
                const choice = await Modals.show<SeriesHeldChoice>(SeriesHeldModal, {
                    status,
                    item,
                })
                if (choice === 'move') moveSeries(lane)
                else if (choice === 'undo' && receipt) undo(receipt)
            })
        }
        // Starting a volume reopened the completed series.
        const reopened = res.series_previous?.status === 'completed' ? res.series_previous : null
        if (reopened && !shown.has(`reopen:${res.series!.id}`)) {
            shown.add(`reopen:${res.series!.id}`)
            useToast().show({
                message: 'Series moved to Reading',
                action: {
                    label: 'Undo',
                    altText: 'Set the status on the series page',
                    onClick: () => restoreSeries(lane, reopened),
                },
            })
        }
        const key = `${res.outcome}:${lane.id}`
        if (!res.previous || shown.has(key)) return
        shown.add(key)
        const receipt = keep()
        useToast().show({
            message: res.outcome === 'completed' ? 'Marked completed' : 'Moved to Reading',
            action: {
                label: 'Undo',
                altText: 'Set the status from the reader menu',
                onClick: () => undo(receipt),
            },
        })
    }

    /**
     * Queues an operation after settling the unsaved reading of the items it writes: reading it
     * `replaces` is dropped (and finishing them means something again); reading it comes `after`
     * goes first, retried if it failed, and if it fails again the operation fails with it.
     */
    function enqueue(job: Job, replaces: string[]) {
        for (const id of replaces) {
            const lane = lanes.get(id)
            if (lane) dropReading(lane)
        }
        for (const id of 'after' in job ? job.after : []) {
            const lane = lanes.get(id)
            if (lane) lane.failed = false
        }
        push(job)
    }

    function command(lane: Lane, req: Command) {
        if (frozen) return Promise.reject(signedOut())
        if (lane.stale && lane.adapter) check(lane)
        const done = Promise.withResolvers<void>()
        const replaces =
            req.op === 'clear' ||
            req.op === 'mark_completed' ||
            (req.op === 'set_status' && req.status === 'completed')
        const ids = [lane.id]
        enqueue(
            { kind: 'command', lane, req, done, after: replaces ? [] : ids },
            replaces ? ids : []
        )
        return done.promise
    }

    function failure(what: string, err: unknown, retry: () => void) {
        useToast().show({
            message: `Couldn't ${what}: ${RequestError.getMessage(err)}`,
            tone: 'danger',
            action: { label: 'Retry', altText: 'Use the reader menu', onClick: retry },
        })
    }

    function restoreSeries(lane: Lane, series: SeriesPrevious) {
        command(lane, { op: 'series_status', series }).catch(err =>
            failure('undo', err, () => restoreSeries(lane, series))
        )
    }

    function moveSeries(lane: Lane) {
        command(lane, { op: 'series_status', status: 'reading' }).catch(err =>
            failure('move the series to Reading', err, () => moveSeries(lane))
        )
    }

    function clear(lane: Lane) {
        command(lane, { op: 'clear' }).then(
            () => {
                place(lane, {})
                useToast().show({ message: 'Cleared the status and position' })
            },
            err => failure('clear', err, () => clear(lane))
        )
    }

    function completeSeries(lane: Lane, seriesId: string, includeUnread: boolean) {
        const done = Promise.withResolvers<void>()
        // Its unread volumes, as this page knows them, are completed with it.
        const covered = !includeUnread
            ? []
            : [...lanes.values()]
                  .filter(l => parentOf(l) === seriesId)
                  .filter(l => l.acked?.status !== 'completed' && l.acked?.status !== 'dropped')
                  .map(l => l.id)
        const after = covered.includes(lane.id) ? [] : [lane.id]
        enqueue({ kind: 'series', lane, seriesId, includeUnread, done, after }, covered)
        done.promise.then(
            () => useToast().show({ message: 'Marked the series completed' }),
            err =>
                failure('complete the series', err, () =>
                    completeSeries(lane, seriesId, includeUnread)
                )
        )
    }

    /** Restores what reading overrode, and stops tracking the item until Track progress. */
    function undo(receipt: number) {
        const r = receipts.get(receipt)
        if (!r || frozen || queue.some(j => j.kind === 'undo' && j.receipt === receipt)) return
        r.lane.tracking = false
        enqueue({ kind: 'undo', lane: r.lane, receipt }, [r.lane.id])
    }

    /** Goes next, holding the item's unsent reading until it is answered. Repeats collapse. */
    function check(lane: Lane) {
        const reading = (j: Job | null) => j?.lane === lane && j.kind === 'read'
        if (frozen || !lane.acked || reading(current) || queue.some(reading)) return
        lane.held = true
        queue.unshift({ kind: 'read', lane })
        pump()
    }

    /** Holds the one dialog slot while `show` runs; no request starts meanwhile. */
    function openDialog(show: () => Promise<void>) {
        if (dialog) return
        dialog = show()
            .catch(err => console.error(err))
            .finally(() => {
                dialog = null
                for (const lane of lanes.values()) if (lane.stale && lane.adapter) check(lane)
                pump()
            })
    }

    /** Until its reader has loaded, it opens there instead. */
    function place(lane: Lane, progress: ReadingProgress) {
        lane.here = progress
        if (lane.ready) lane.adapter?.restore(progress)
    }

    function compare(lane: Lane, remote: ReadingStateResponse) {
        const adapter = lane.adapter!
        const acked = lane.acked!
        const next = remote.state
        const here = lane.here ?? acked.progress
        if (isCleared(next) && !isCleared(acked)) return resolve(lane, 'reset', remote, here)
        if (next.status === 'completed' && acked.status !== 'completed') {
            return resolve(lane, 'completed', remote, here)
        }
        if (adapter.samePosition(next.progress, acked.progress)) {
            adopt(lane, remote)
            if (next.status !== acked.status) {
                useToast().show({
                    message: next.status
                        ? `Marked ${READING_STATUS_LABELS[next.status]} on another device`
                        : 'Status cleared on another device',
                    tone: 'info',
                })
            }
            return
        }
        if (hasReading(lane)) return resolve(lane, 'moved', remote, here)
        adopt(lane, remote)
        place(lane, next.progress)
        useToast().show({
            message: `Moved to ${adapter.describe(next.progress)} from another device`,
            tone: 'info',
            action: {
                label: 'Undo',
                altText: 'Go back with the page controls',
                onClick: () => void (lane.adapter === adapter && place(lane, here)),
            },
        })
    }

    function resolve(
        lane: Lane,
        kind: 'moved' | 'completed' | 'reset',
        remote: ReadingStateResponse,
        here: ReadingProgress
    ) {
        const adapter = lane.adapter!
        lane.held = true
        openDialog(async () => {
            const choice = await Modals.show<ConflictChoice>(ReadingConflictModal, {
                kind,
                here: adapter.describe(here),
                saved: adapter.describe(remote.state.progress),
            })
            // Stay, Keep reading and Continue here send what's retained on top of it.
            adopt(lane, remote)
            lane.held = false
            if (choice === 'go' || choice === 'start') {
                dropReading(lane)
                place(lane, choice === 'go' ? remote.state.progress : {})
            } else if (choice === 'reset' && (await showClearReadingModal(lane.id, true))) {
                clear(lane)
            }
        })
    }

    function attach(contentId: string, adapter: ReaderAdapter) {
        const lane = laneOf(contentId)
        const mine = () => lane.adapter === adapter && !frozen
        const bump = () => {
            const content = adapter.content()
            if (content) bumpContinueReading(content)
        }

        return reactive({
            acked: computed(() => lane.acked),
            series: computed(() => lane.series),
            failures: computed(() => lane.failures),
            stale: computed(() => lane.stale),
            tracking: computed(() => lane.tracking),

            /** Once the reader can compare positions: reads the saved state after every write
             * queued before, and reconciles what came in meanwhile. Resolves to where the reader
             * opens, which is where it was if reading here is still unsaved. */
            load() {
                if (frozen) return Promise.reject(signedOut())
                lane.adapter = adapter
                lane.ready = false
                const done = Promise.withResolvers<ReadingProgress>()
                push({ kind: 'read', lane, done })
                return done.promise
            },
            /** Real reading, written once the reader pauses. */
            moved(progress: ReadingProgress) {
                if (!mine() || !lane.tracking) return
                const finished = finishedAt(lane)
                if (finished && adapter.samePlace(progress, finished)) return
                lane.here = progress
                lane.failed = false
                bump()
                const last = queue.at(-1)
                if (last?.kind === 'position' && last.lane === lane && !last.sealed) {
                    last.progress = progress
                } else {
                    push({ kind: 'position', lane, progress, sealed: false })
                }
                if (timer) clearTimeout(timer)
                timer = setTimeout(sendNow, WRITE_DEBOUNCE)
            },
            placed(progress: ReadingProgress) {
                if (!mine()) return
                lane.here = progress
                sendNow()
            },
            finish(progress: ReadingProgress) {
                if (!mine() || !lane.tracking || finishedAt(lane)) return
                lane.here = progress
                lane.failed = false
                bump()
                push({ kind: 'finish', lane, progress })
            },
            flush: sendNow,
            command: (req: Exclude<Command, { op: 'series_status' }>) => command(lane, req),
            seriesCommand: (status: ReadingStatus) =>
                command(lane, { op: 'series_status', status }),
            resetAndReadAgain: () =>
                openDialog(async () => {
                    if (await showClearReadingModal(lane.id, true)) clear(lane)
                }),
            completeSeries() {
                const series = lane.series
                if (!series) return
                const unread =
                    series.children_count -
                    series.completed_children_count -
                    series.dropped_children_count
                const type = adapter.content()?.type === 'book' ? 'book_series' : 'comic_series'
                openDialog(async () => {
                    const choice = await showMarkSeriesCompletedModal(series.id, { type, unread })
                    if (choice) completeSeries(lane, series.id, choice.includeUnread)
                })
            },
            trackProgress() {
                lane.tracking = true
            },
            /** The failures stay counted, and shown, until the retried reading is saved. */
            retry() {
                lane.failed = false
                sendNow()
            },
            check() {
                if (mine()) check(lane)
            },
            /** The page may be closing: unsent reading goes with keepalive if nothing is out. */
            hide() {
                seal()
                keepalive = !current
                pump()
                keepalive = false
            },
            detach() {
                if (lane.adapter !== adapter) return
                const content = adapter.content()
                lane.content = content && { title: content.title, parent_id: content.parent_id }
                lane.adapter = null
                sendNow()
                if (lane.dirty) {
                    lane.dirty = false
                    void invalidateReading(lane.id, parentOf(lane))
                }
            },
        })
    }

    return {
        attach,
        teardown() {
            freeze()
            unsubscribe()
        },
    }
}

let actor: ReturnType<typeof createReadingActor> | null = null

export function attachReading(contentId: string, adapter: ReaderAdapter) {
    actor ??= createReadingActor()
    return actor.attach(contentId, adapter)
}

/** For tests: the next reader starts a fresh actor. */
export function resetReadingActor() {
    actor?.teardown()
    actor = null
}

export type ReadingSync = ReturnType<typeof attachReading>
