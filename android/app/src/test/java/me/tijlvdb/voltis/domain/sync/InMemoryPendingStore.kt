package me.tijlvdb.voltis.domain.sync

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.map
import me.tijlvdb.voltis.domain.storage.StorageFullException

/** A [PendingStore] over a map, with the same row rules as the Room one (`after`), and faults on demand. */
class InMemoryPendingStore : PendingStore {
    val rows = MutableStateFlow<Map<String, PendingUserData>>(emptyMap())
    val notices = mutableListOf<StoredNotice>()
    val commits = mutableListOf<PendingChange>()

    /** Refused commits, as a full disk refuses them: [fullFor] picks which. */
    var fullFor: (PendingChange) -> Boolean = { false }
    var pruneFails = false

    /** A prune or a commit waits for its gate first, cancellably, before it changes anything. */
    var pruneGate: CompletableDeferred<Unit>? = null
    var commitGate: CompletableDeferred<Unit>? = null

    override suspend fun wanted() = rows.value.values.filter { it.wanted }.sortedWith(compareBy({ it.createdAt }, { it.contentId }))

    override fun watchWanted(): Flow<List<PendingUserData>> = rows.map { all -> all.values.filter { it.wanted }.sortedBy { it.createdAt } }

    override fun watch(contentId: String): Flow<PendingUserData?> = rows.map { it[contentId] }

    override suspend fun landedSeq() = rows.value.values.maxOfOrNull { it.landed?.seq ?: 0 } ?: 0

    override suspend fun prune() {
        pruneGate?.await()
        if (pruneFails) throw StorageFullException()
        rows.value = rows.value.filterValues { it.wanted }.mapValues { (_, row) -> row.copy(landed = null) }
    }

    override suspend fun commit(change: PendingChange): List<StoredNotice> {
        commitGate?.await()
        if (fullFor(change)) throw StorageFullException()
        commits += change
        val before = rows.value[change.contentId]
        val after = before.after(change, landedSeq() + 1)
        rows.value = if (after == null) rows.value - change.contentId else rows.value + (change.contentId to after)
        if (change is PendingChange.Dropped && before.drops(change)) return listOf(change.notice.copy(id = notices.size + 1L)).also { notices += it }
        return emptyList()
    }
}
