package me.tijlvdb.voltis.domain.reading

import java.time.Instant
import java.time.OffsetDateTime
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import me.tijlvdb.voltis.data.api.UserData

/**
 * When something last happened to a volume's reading. An unsent op's change has no server time, and the
 * server will stamp it after everything the phone knows: it is [Pending], by its place in the queue, and
 * sorts after every [Server] time. No clock is read.
 */
sealed interface Recency : Comparable<Recency> {
    data class Server(val at: Instant) : Recency

    data class Pending(val rank: Int) : Recency

    override fun compareTo(other: Recency): Int = when {
        this is Server && other is Server -> at.compareTo(other.at)
        this is Pending && other is Pending -> rank.compareTo(other.rank)
        this is Pending -> 1
        else -> -1
    }
}

/**
 * What the phone knows of one content's reading: the server's newest state it has ([seq]) with the outbox's
 * unsent ops applied in order. [pending]: an unsent op touches it. [lastReadAt] and [progress] are for display
 * (an unsent op's time is its own); decisions read [recency].
 */
data class EffectiveReading(
    val status: String?,
    val progress: JsonObject,
    val lastReadAt: String?,
    val stamps: Stamps,
    val seq: Long,
    val pending: Boolean = false,
    /** The snapshot's own revision and progress time: [overlay] carries them with [seq], so a page never mixes two states' identities. */
    val revision: String? = null,
    val progressUpdatedAt: String? = null,
) {
    /** The later of the last read and the last status change: Continue's anchor order. Null when neither ever happened. */
    val recency: Recency? get() = listOfNotNull(
        stamps.lastReadRank?.let(Recency::Pending), stamps.statusRank?.let(Recency::Pending),
        stamps.lastReadAt?.let(::server), stamps.statusUpdatedAt?.let(::server),
    ).maxOrNull()

    /** [data] with this reading over its status, progress and times, for a page that shows the item. */
    fun overlay(data: UserData?) = (data ?: UserData()).copy(
        status = status, progress = progress, lastReadAt = lastReadAt, statusUpdatedAt = stamps.statusUpdatedAt, readingSeq = seq,
        revision = revision, progressUpdatedAt = progressUpdatedAt,
    )

    private fun server(time: String): Recency = Recency.Server(runCatching { OffsetDateTime.parse(time).toInstant() }.getOrDefault(Instant.EPOCH))
}

/**
 * Every content an unsent op reads or writes: its own, the volumes its guard covers, and its series (the payload's,
 * or its lane's [laneParent]). Their snapshots and lanes are kept for as long as the op is.
 */
fun Op.references(laneParent: String?): Set<String> =
    setOfNotNull(contentId, laneParent, (payload["series_id"] as? JsonPrimitive)?.contentOrNull) + guard?.guardVolumes().orEmpty()

/**
 * [ids]' effective readings from the stored inputs of one consistent read: [snapshots] (the server's newest
 * states), the [lanes] that identify content (their type, series and page count; never their base), and the
 * outbox [ops] in order. An id without a snapshot is unknown and has no entry: an untouched content has
 * one, at seq 0. The base of every op is the snapshot, never a lane's own copy of the server's state.
 */
fun effectiveReading(ids: Collection<String>, snapshots: Map<String, ReadingState>, lanes: Map<String, Lane>, ops: List<Op>): Map<String, EffectiveReading> {
    val bases = (ids.toSet() + ops.flatMap { it.references(lanes[it.contentId]?.parentId) }).filter { it in snapshots }
    val based = bases.associateWith { (lanes[it] ?: Lane(it)).copy(state = snapshots.getValue(it)) }
    val result = projected(ops, based)
    val touched = ops.flatMap { listOf(it.contentId) + it.guard?.guardVolumes().orEmpty() }.toSet()
    return ids.filter { it in snapshots }.associateWith { id ->
        val state = snapshots.getValue(id)
        val shown = result.shown[id]
        EffectiveReading(
            // By whether an op projects it: a projected null (a cleared status, time) stays null.
            status = if (shown != null) shown.status else state.status,
            progress = if (shown != null) shown.progress else state.progress,
            lastReadAt = if (shown != null) shown.lastReadAt else state.lastReadAt,
            stamps = result.stamps[id] ?: Stamps(state.lastReadAt, null, state.statusUpdatedAt, null),
            seq = state.seq,
            pending = shown != null || id in touched,
            revision = state.revision,
            progressUpdatedAt = state.progressUpdatedAt,
        )
    }
}
