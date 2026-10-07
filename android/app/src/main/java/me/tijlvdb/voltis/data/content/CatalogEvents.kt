package me.tijlvdb.voltis.data.content

import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.filterIsInstance
import kotlinx.coroutines.flow.map

sealed interface CatalogChange {
    /** An item's reading changed. [parentId] is the item's series, or the item itself without one. */
    data class ReadingChanged(val contentId: String, val parentId: String) : CatalogChange

    /** A reader's position was saved, and nothing else changed: only Home follows it (the web's `markPositionSaved`). */
    data class PositionSaved(val contentId: String, val parentId: String) : CatalogChange

    /** A star or a rating changed. */
    data class UserDataChanged(val contentId: String) : CatalogChange

    data object PreferencesChanged : CatalogChange

    /** The server became reachable, or stopped being: every screen reloads once (P2 §10). */
    data class OnlineChanged(val online: Boolean) : CatalogChange
}

sealed interface CatalogSignal {
    /** [batch]: the open bulk batch that produced this, or null. A stamped change is held until that batch ends. */
    data class Changed(val change: CatalogChange, val batch: Long?) : CatalogSignal

    /** Batch [batch] is over. [touched]: one ReadingChanged per command, for items changed without an event (conflict, 404). */
    data class BatchEnded(val batch: Long, val touched: List<CatalogChange>) : CatalogSignal
}

/** What this app changed on the server, so open screens can refresh: the web's query invalidation. */
@Singleton
class CatalogEvents @Inject constructor() {
    // Unbounded: while subscribed, tryEmit never fails. No replay: a screen collects for as long as it is on a back stack.
    private val _signals = MutableSharedFlow<CatalogSignal>(extraBufferCapacity = Int.MAX_VALUE)
    val signals: SharedFlow<CatalogSignal> = _signals.asSharedFlow()

    /** The changes alone (any reader that doesn't care about batches). */
    val changes: Flow<CatalogChange> = signals.filterIsInstance<CatalogSignal.Changed>().map { it.change }

    private var open: Long? = null // guarded by this

    /** [origin]: the bulk batch whose command produced [change]. Kept only while that batch is open; otherwise ordinary. */
    fun emit(change: CatalogChange, origin: Long? = null) = synchronized(this) {
        check(_signals.tryEmit(CatalogSignal.Changed(change, origin?.takeIf { it == open })))
    }

    /** Only BulkRunner's job body calls it. */
    fun begin(id: Long) = synchronized(this) {
        check(open == null)
        open = id
    }

    /** Idempotent: only the open batch's end is emitted, once. */
    fun end(id: Long, touched: List<CatalogChange>) = synchronized(this) {
        if (open != id) return
        open = null
        check(_signals.tryEmit(CatalogSignal.BatchEnded(id, touched)))
    }
}
