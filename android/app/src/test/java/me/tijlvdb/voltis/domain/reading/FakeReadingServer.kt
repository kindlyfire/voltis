package me.tijlvdb.voltis.domain.reading

import kotlinx.coroutines.CompletableDeferred
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.longOrNull
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.data.api.UserData

/**
 * For tests: one user's reading rows behind [ReadingTransport], applying `Transition.kt` and the
 * revision contract of reading.go, with controls for holding, failing and losing requests. The port
 * of the web's fakeReadingServer.ts, plus what it lacks: [volumes], `series-reading` with its
 * writer-and-seq rule, and a series clear that clears its volumes. Items end on page [last].
 */
class FakeReadingServer(private val last: Int) : ReadingTransport {
    /** [path] is `reading`, `series-reading`, `get` or `volumes`; [body] is null for reads. */
    data class Sent(val id: String, val path: String, val body: JsonObject?)

    val rows = mutableMapOf<String, ReadingState>()

    /** Volume to series, in the series' order. */
    val parents = linkedMapOf<String, String>()

    /** The series among the IDs; everything else is a comic. */
    val series = mutableSetOf<String>()

    /** Volumes the list leaves out, as it does invalid ones. */
    val invalid = mutableSetOf<String>()

    /** A Voltis 500 for every request: the web's `offline`, its failure. */
    var failing = false

    /** No answer at all. */
    var unreachable = false

    /** Requests that fail as [failing] does. */
    var refuse: (Sent) -> Boolean = { false }

    /** Requests answered with the failure it returns: a 401, a 404, a refusal. */
    var failWith: (Sent) -> ReadingFailure? = { null }

    /** The next write lands, but answers a 500. */
    var loseAck = false

    var holding = false

    /** While [holding], the requests that are held. */
    var holds: (Sent) -> Boolean = { true }
    private val held = mutableListOf<CompletableDeferred<Unit>>()
    var inFlight = 0
        private set
    var maxInFlight = 0
        private set
    val sent = mutableListOf<Sent>()
    private var n = 0
    private var seqs = 0L

    fun stateOf(id: String) = rows[id] ?: ReadingState()

    /** Writes [id] as the server or another writer would. */
    fun set(id: String, rev: String = "srv:${++n}", change: ReadingState.() -> ReadingState) {
        // Every write of a row takes the next reading_seq.
        rows[id] = stateOf(id).change().copy(revision = rev, seq = ++seqs)
    }

    /** Registers [seriesId] with its [volumes], in order. */
    fun series(seriesId: String, vararg volumes: String) {
        series += seriesId
        for (volume in volumes) parents[volume] = seriesId
    }

    /** Clears the item as another device's clear does. */
    fun clear(id: String) = clearContent(id, "srv:${++n}")

    /** reading.go's clearContent: the item and every volume of a series, valid or not, under one revision. */
    private fun clearContent(id: String, rev: String) {
        for (target in listOf(id) + parents.filterValues { it == id }.keys) set(target, rev) { ReadingState() }
    }

    fun release() {
        holding = false
        held.toList().also { held.clear() }.forEach { it.complete(Unit) }
    }

    private fun kids(seriesId: String) = parents.filterValues { it == seriesId }.keys.filter { it !in invalid }

    private fun writerOf(rev: String?) = rev?.takeIf { it.startsWith("t") }?.substringBefore(':')

    private fun seqOf(rev: String?) = rev?.substringAfter(':', "")?.toLongOrNull()

    private fun env(id: String): Envelope {
        val series = parents[id]?.let { parent ->
            val statuses = kids(parent).map { stateOf(it).status }
            val own = stateOf(parent)
            SeriesInfo(
                parent, own.status, own.revision,
                statusUpdatedAt = own.statusUpdatedAt, progress = own.progress, progressUpdatedAt = own.progressUpdatedAt, lastReadAt = own.lastReadAt, seq = own.seq,
                caughtUp = statuses.isNotEmpty() && statuses.all { it == ReadingStatus.COMPLETED || it == ReadingStatus.DROPPED },
                childrenCount = statuses.size,
                completedChildrenCount = statuses.count { it == ReadingStatus.COMPLETED },
                droppedChildrenCount = statuses.count { it == ReadingStatus.DROPPED },
            )
        }
        // Keys sorted, as Go encodes a map: the order may differ from the client's for equal progress.
        val state = stateOf(id).let { it.copy(progress = JsonObject(it.progress.toSortedMap())) }
        return Envelope(state, series, writerOf(state.revision))
    }

    /** The series moves to Reading from no status or planned, and with [reopen] from completed too. */
    private fun startSeries(parent: String?, rev: String, reopen: Boolean): SeriesPrevious? {
        parent ?: return null
        val cur = stateOf(parent)
        if (cur.status != null && cur.status != ReadingStatus.PLAN_TO_READ && !(reopen && cur.status == ReadingStatus.COMPLETED)) return null
        set(parent, rev) { copy(status = ReadingStatus.READING, statusUpdatedAt = NOW) }
        return SeriesPrevious(rev, cur.status, cur.statusUpdatedAt)
    }

    private fun revertSeries(seriesId: String, prev: SeriesPrevious, rev: String) {
        if (stateOf(seriesId).revision == prev.revision) set(seriesId, rev) { copy(status = prev.status, statusUpdatedAt = prev.statusUpdatedAt) }
    }

    private fun end(id: String) = endProgress(if (id in series) ContentType.COMIC_SERIES else ContentType.COMIC, last + 1)

    /** reading.go's writeReading for one row: [op] through [transition], keeping times that didn't change. */
    private fun write(id: String, op: ReadingOp, rev: String): Transition {
        val cur = stateOf(id)
        val t = transition(Snapshot(cur.status, cur.progress, cur.lastReadAt), op, end(id), NOW)
        if (!t.write) return t
        set(id, rev) {
            copy(
                status = t.next.status,
                statusUpdatedAt = if (t.next.status != cur.status) NOW else statusUpdatedAt,
                progress = t.next.progress,
                progressUpdatedAt = when {
                    t.outcome == Outcome.STATUS_SET -> progressUpdatedAt
                    t.next.progress.isEmpty() -> null
                    else -> NOW
                },
                lastReadAt = t.next.lastReadAt,
            )
        }
        return t
    }

    private fun apply(id: String, body: JsonObject): ReadingResult {
        val op = body.string("op")!!
        val writer = body.string("writer_id")
        val seq = (body["seq"] as? JsonPrimitive)?.longOrNull ?: 0
        val rev = if (writer != null) "$writer:$seq" else "srv:${++n}"
        val cur = stateOf(id)
        fun result(outcome: String, previous: Snapshot? = null, seriesPrevious: SeriesPrevious? = null) =
            env(id).let {
                // A series clear lists its children's states, those the client guarded too.
                val items = if (op == "clear" && id in series) (parents.filterValues { c -> c == id }.keys + body.ids()).sorted().map(::item) else emptyList()
                ReadingResult(it.state, it.series, it.writer, outcome, previous, seriesPrevious, items)
            }

        if (writer == null && op in listOf("position", "finish", "restore", "series_status")) {
            throw ReadingFailure.Refused(400, "$op needs writer_id and seq")
        }
        if (op == "series_status") {
            val parent = parents[id] ?: throw ReadingFailure.Refused(400, "Not in a series")
            val revision = stateOf(parent).revision
            if (writerOf(revision) == writer && seq <= (seqOf(revision) ?: 0)) return result(Outcome.NONE)
            val prev = body["series"].decode<SeriesPrevious>()
            if (prev != null) revertSeries(parent, prev, rev) else write(parent, ReadingOp("set_status", body.string("status")), rev)
            return result(Outcome.SERIES_STATUS)
        }
        if (writer != null) {
            if (writerOf(cur.revision) == writer && seq <= (seqOf(cur.revision) ?: 0)) return result(Outcome.NONE)
            if (cur.revision != body.string("base_revision")) throw ReadingFailure.Conflict(env(id))
        }
        if (op == "clear") {
            clearContent(id, rev)
            return result(Outcome.CLEARED)
        }
        val progress = (body["progress"] as? JsonObject)?.let { if (op == "position") positionProgress(it) else it }
        val readingOp = ReadingOp(op, body.string("status"), progress, body["snapshot"].decode<Snapshot>())
        val t = write(id, readingOp, rev)
        if (!t.write) return result(t.outcome)
        var seriesPrevious: SeriesPrevious? = null
        if (startsSeries(readingOp, t.outcome)) {
            seriesPrevious = startSeries(parents[id], rev, reopen = t.outcome == Outcome.STARTED || t.outcome == Outcome.MOVED_TO_READING)
        } else if (t.outcome == Outcome.RESTORED) {
            body["series"].decode<SeriesPrevious>()?.let { prev -> parents[id]?.let { revertSeries(it, prev, rev) } }
        }
        return result(t.outcome, t.previous, seriesPrevious)
    }

    private fun item(id: String) = stateOf(id).let { ReadingItem(id, it.revision, it.status, it.statusUpdatedAt, it.progress, it.progressUpdatedAt, it.lastReadAt, it.seq) }

    private fun JsonObject.ids() = (this["ids"] as? JsonArray)?.map { (it as JsonPrimitive).content }.orEmpty()

    /** The series and the volumes the action covers, with the client's `ids`, in their states now. */
    private fun receipt(seriesId: String, body: JsonObject, count: Int): SeriesReceipt {
        val covered = when (body.string("action")) {
            "clear" -> parents.filterValues { it == seriesId }.keys
            "mark_series_completed" -> if ((body["include_unread"] as? JsonPrimitive)?.booleanOrNull == true) kids(seriesId).toSet() else emptySet()
            else -> kids(seriesId).take(kids(seriesId).indexOf(body.string("until_id")) + 1).toSet()
        }
        return SeriesReceipt(count, item(seriesId), (covered + body.ids()).sorted().map(::item))
    }

    private fun applySeries(seriesId: String, body: JsonObject): SeriesReceipt {
        val writer = body.string("writer_id")
        val seq = (body["seq"] as? JsonPrimitive)?.longOrNull ?: 0
        val rev = if (writer != null) "$writer:$seq" else "srv:${++n}"
        val all = listOf(seriesId) + parents.filterValues { it == seriesId }.keys
        // Delivered already, or overtaken by this writer's later write to the series or a volume: nothing changes, the receipt is the same.
        if (writer != null && all.any { writerOf(stateOf(it).revision) == writer && (seqOf(stateOf(it).revision) ?: 0) >= seq }) return receipt(seriesId, body, 0)
        fun unfinished(ids: List<String>) = ids.filter { stateOf(it).status != ReadingStatus.COMPLETED && stateOf(it).status != ReadingStatus.DROPPED }
        val completing = when (val action = body.string("action")) {
            "clear" -> {
                clearContent(seriesId, rev)
                return receipt(seriesId, body, 1 + parents.count { it.value == seriesId })
            }
            "mark_through" -> {
                val volumes = kids(seriesId)
                val until = volumes.indexOf(body.string("until_id"))
                if (until < 0) throw ReadingFailure.Gone("Volume not found")
                unfinished(volumes.take(until + 1))
            }
            "mark_series_completed" ->
                (if ((body["include_unread"] as? JsonPrimitive)?.booleanOrNull == true) unfinished(kids(seriesId)) else emptyList()) + seriesId
            else -> throw ReadingFailure.Refused(400, "Unknown action $action")
        }
        for (id in completing) {
            val progress = end(id)
            set(id, rev) {
                copy(
                    status = ReadingStatus.COMPLETED,
                    statusUpdatedAt = if (status != ReadingStatus.COMPLETED) NOW else statusUpdatedAt,
                    progress = progress,
                    progressUpdatedAt = if (progress.isEmpty()) null else NOW,
                )
            }
        }
        if (body.string("action") == "mark_through" && completing.isNotEmpty()) startSeries(seriesId, rev, reopen = false)
        return receipt(seriesId, body, completing.size)
    }

    private suspend fun <T> call(sent: Sent, block: () -> T): T {
        this.sent += sent
        maxInFlight = maxOf(maxInFlight, ++inFlight)
        try {
            if (holding && holds(sent)) CompletableDeferred<Unit>().also { held += it }.await()
            if (unreachable) throw ReadingFailure.Unreachable()
            if (failing || refuse(sent)) throw ReadingFailure.ServerError(500, "test")
            failWith(sent)?.let { throw it }
            val result = block()
            if (sent.body != null && loseAck) {
                loseAck = false
                throw ReadingFailure.ServerError(500, "lost")
            }
            return result
        } finally {
            inFlight--
        }
    }

    override suspend fun get(contentId: String, quick: Boolean) = call(Sent(contentId, "get", null)) { env(contentId) }

    override suspend fun post(contentId: String, body: JsonObject) = call(Sent(contentId, "reading", body)) { apply(contentId, body) }

    override suspend fun seriesReading(seriesId: String, body: JsonObject) =
        call(Sent(seriesId, "series-reading", body)) { applySeries(seriesId, body) }

    override suspend fun volumes(seriesId: String) = call(Sent(seriesId, "volumes", null)) {
        SeriesVolumes(stateOf(seriesId).revision, kids(seriesId).map { VolumeRef(it, rows[it]?.userData()) }, stateOf(seriesId).userData())
    }

    private fun JsonObject.string(key: String) = (this[key] as? JsonPrimitive)?.contentOrNull

    private inline fun <reified T> JsonElement?.decode(): T? =
        this?.takeIf { it !is JsonNull }?.let { AppJson.decodeFromJsonElement<T>(it) }

    companion object {
        /** The time of every write here. */
        const val NOW = "now"
    }
}
