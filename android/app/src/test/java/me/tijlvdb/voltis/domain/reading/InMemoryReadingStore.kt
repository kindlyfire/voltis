package me.tijlvdb.voltis.domain.reading

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.domain.sync.StoredNotice

/** For tests: a [ReadingStore] in memory, held to Room's by `ReadingStoreContractTest`. */
class InMemoryReadingStore(private val announce: suspend (List<StoredNotice>) -> Unit = {}) : ReadingStore {
    private data class Data(
        val lanes: Map<String, Lane> = emptyMap(),
        /** In id order. */
        val ops: Map<Long, Op> = emptyMap(),
        val notices: List<StoredNotice> = emptyList(),
        val lastId: Long = 0,
        val snapshots: Map<String, ReadingState> = emptyMap(),
    )

    private val data = MutableStateFlow(Data())

    /** Cached content rows with their positions, as `content` holds them. */
    private val cached = mutableMapOf<String, Pair<Content, Int?>>()

    /** Items with a download row. */
    val downloads = mutableSetOf<String>()

    /** Series whose whole list was cached (`volumes_known`). */
    private val known = mutableSetOf<String>()

    /** As `cache()` does: the rows are metadata, their reading states are imported; [wholeList] is a series whose list this is. */
    suspend fun cache(rows: List<Pair<Content, Int?>>, wholeList: String? = null) {
        for ((content, position) in rows) cached[content.id] = content to position
        import(rows.map { (c, _) -> snapshotOf(c.id, c.userData) })
        wholeList?.let { known += it }
    }

    private fun merged(base: Map<String, ReadingState>, states: Collection<ReadingSnapshot>): Map<String, ReadingState> {
        val out = base.toMutableMap()
        // Insert when absent, replace only for a greater seq.
        for ((id, state) in states) if (out[id]?.let { state.seq > it.seq } != false) out[id] = state
        return out
    }

    /** The outbox as stored. */
    val ops get() = data.value.ops.values.toList()

    val notices get() = data.value.notices

    override suspend fun load(): Stored {
        val d = data.value
        val ids = d.ops.values.map { it.contentId }.toSet()
        return Stored(d.ops.values.toList(), d.lanes.filterValues { it.contentId in ids || it.needsReview })
    }

    override suspend fun lane(contentId: String) = data.value.lanes[contentId]

    override suspend fun dismissNotice(id: Long) {
        data.value = data.value.let { d -> d.copy(notices = d.notices.filter { it.id != id }) }
    }

    override suspend fun commit(change: Change): List<Long> {
        val d = data.value
        val ops = LinkedHashMap(d.ops)
        change.deleteOps.forEach(ops::remove)
        var lastId = d.lastId
        val ids = mutableListOf<Long>()
        for (op in change.ops) {
            if (op.id == 0L) {
                ids += ++lastId
                ops[lastId] = op.copy(id = lastId)
            } else {
                check(op.id in ops) { "No such op" }
                ops[op.id] = op
            }
        }
        var noticeId = d.notices.maxOfOrNull { it.id } ?: 0
        val stored = change.notices.map { it.copy(id = ++noticeId) }
        data.value = Data(
            d.lanes - change.deleteLanes.toSet() + change.lanes.associateBy { it.contentId }, ops, d.notices + stored, lastId,
            merged(d.snapshots, change.snapshots),
        )
        if (stored.isNotEmpty()) announce(stored)
        return ids
    }

    override suspend fun volumes(seriesId: String): SeriesVolumes? {
        if (seriesId !in known) return null
        val snapshots = data.value.snapshots
        val own = snapshots[seriesId] ?: return null
        val volumes = cached.values.filter { (c, _) -> c.parentId == seriesId && c.valid }.sortedWith(compareBy({ it.second }, { it.first.id }))
        if (volumes.any { it.second == null }) return null
        return SeriesVolumes(own.revision, volumes.map { (c, _) -> VolumeRef(c.id, (snapshots[c.id] ?: return null).userData()) })
    }

    override suspend fun import(snapshots: Collection<ReadingSnapshot>) {
        data.value = data.value.let { it.copy(snapshots = merged(it.snapshots, snapshots)) }
    }

    override suspend fun snapshot(contentId: String) = data.value.snapshots[contentId]

    override suspend fun effective(ids: Collection<String>): Map<String, EffectiveReading> =
        data.value.let { effectiveReading(ids, it.snapshots, it.lanes, it.ops.values.toList()) }

    /** A catalog refresh dropping rows nothing needs, as `ContentDao.deleteUnneeded` does: no download, no op of their own. */
    fun uncache(ids: Collection<String>) {
        val own = data.value.ops.values.map { it.contentId }.toSet()
        cached.keys.removeAll { it in ids && it !in downloads && it !in own }
    }

    override suspend fun prune(before: Long) {
        val d = data.value
        val ids = d.ops.values.map { it.contentId }.toSet()
        // What the unsent ops read or write: their content, the volumes their guards cover, their series.
        val refs = d.ops.values.flatMap { it.references(d.lanes[it.contentId]?.parentId) }.toSet()
        val lanes = d.lanes.filterValues { it.touchedAt >= before || it.needsReview || it.contentId in ids || it.contentId in downloads || it.contentId in refs }
        // Snapshots outlive lanes: only content rows, downloads, what unsent ops reference and held reviews keep them.
        val kept = cached.keys + downloads + refs + d.lanes.filterValues { it.needsReview }.keys
        data.value = d.copy(lanes = lanes, snapshots = d.snapshots.filterKeys { it in kept })
    }

    override fun shown(ids: Set<String>): Flow<Map<String, Shown>> = data.map { d ->
        val unsent = d.ops.values.map { it.contentId }.toSet()
        d.lanes.filterKeys { it in ids }.mapValues { (id, l) ->
            // By text, as Room's SQL compares them.
            val projected = l.shownStatus != l.state.status || l.shownProgress.toString() != l.state.progress.toString() || l.shownLastReadAt != l.state.lastReadAt
            Shown(l.shownStatus, l.shownProgress, l.shownLastReadAt, id in unsent, l.needsReview, projected)
        }
    }.distinctUntilChanged()

    override fun unsent(): Flow<Int> = data.map { it.ops.size }.distinctUntilChanged()

    override fun held(): Flow<List<AttentionItem.Held>> = data.map { d ->
        fun titleOf(id: String?) = id?.let { d.lanes[it]?.title ?: cached[it]?.first?.title }
        d.lanes.values.filter { it.needsReview }.map { Triple(it.contentId, it.title ?: it.contentId, titleOf(it.parentId)) }
            .sortedWith(compareBy({ it.third?.lowercase() }, { it.second.lowercase() }, { it.first }))
            .map { (id, title, series) -> AttentionItem.Held(id, titled(series, title)) }
    }.distinctUntilChanged()

    override suspend fun title(contentId: String) = data.value.lanes[contentId]?.title ?: cached[contentId]?.first?.title
}
