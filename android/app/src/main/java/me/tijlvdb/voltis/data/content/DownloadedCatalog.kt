package me.tijlvdb.voltis.data.content

import javax.inject.Inject
import javax.inject.Singleton
import androidx.room.withTransaction
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.withContext
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.db.AccountStores
import me.tijlvdb.voltis.data.db.ContentEntity
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.data.reading.effectiveReading
import me.tijlvdb.voltis.data.reading.effectiveReadings
import me.tijlvdb.voltis.domain.catalog.VolumeReading
import me.tijlvdb.voltis.domain.reading.EffectiveReading

/**
 * The open account's cached content rows (P2 §3), for pages that work without the server. A row is
 * metadata; its reading is the effective one: the newest server state the phone knows with the
 * outbox's unsent ops applied, the same for every page and decision that reads it.
 */
@Singleton
class DownloadedCatalog internal constructor(private val db: () -> VoltisDatabase?) {
    @Inject
    constructor(stores: AccountStores) : this({ stores.current.value?.db })

    /**
     * A series' valid volumes in the server's order. Null when its list isn't known whole (a series
     * listed with no volumes is known: it has none), one was cached without its place in the list,
     * or the reading of one is unknown.
     */
    suspend fun volumes(seriesId: String): List<Content>? =
        withContext(Dispatchers.IO) { db()?.let { series(it, seriesId) }?.map { it.content.withReading(it.reading) } }

    /** [volumes] with their readings, again whenever any of it changes: what offline Continue is resolved from. */
    fun series(seriesId: String): Flow<List<VolumeReading>?> {
        val db = db() ?: return flowOf(null)
        return db.invalidationTracker.createFlow("content", "reading_snapshot", "reading_lane", "reading_op").map { series(db, seriesId) }
    }

    /** [ids]' effective readings, again whenever a snapshot, a lane, an op or a content row changes: what a cached page shows. */
    fun effective(ids: Set<String>): Flow<Map<String, EffectiveReading>> = db()?.effectiveReadings(ids) ?: flowOf(emptyMap())

    /** Membership, order, snapshots and ops of one moment: a refresh can't commit between them. */
    private suspend fun series(db: VoltisDatabase, seriesId: String): List<VolumeReading>? = db.withTransaction {
        val series = db.content().get(seriesId)?.takeIf { it.volumesKnown } ?: return@withTransaction null
        val rows = db.content().volumes(series.id)
        if (rows.any { it.position == null }) return@withTransaction null
        val readings = db.effectiveReading(rows.map { it.id })
        rows.map { VolumeReading(it.toContent() ?: return@withTransaction null, readings[it.id] ?: return@withTransaction null) }
    }

    /**
     * A cached row as it was fetched, its reading not included, for a first frame. Read from [db], the store
     * a page captured when it opened, not whichever is open when the read runs.
     */
    suspend fun fetchedRow(db: VoltisDatabase, id: String): Content? = withContext(Dispatchers.IO) { db.content().get(id)?.toContent() }

    /** Any cached row, with its effective reading. */
    suspend fun stored(id: String): Content? = withContext(Dispatchers.IO) {
        val db = db() ?: return@withContext null
        db.withTransaction { db.content().get(id)?.toContent()?.withReading(db.effectiveReading(listOf(id))[id]) }
    }

    /**
     * What an offline page shows (P2 §10): a downloaded item, or a series with a downloaded volume, as last
     * fetched with its effective reading. Null for anything else.
     */
    suspend fun content(id: String): Content? = withContext(Dispatchers.IO) {
        val db = db() ?: return@withContext null
        val downloads = db.downloads()
        val offline = downloads.get(id)?.copyId != null || downloads.inSeries(id).any { it.copyId != null }
        if (offline) stored(id) else null
    }
}

/** The row as fetched, its reading not included: it is in `reading_snapshot`. Null when its JSON can't be read. */
fun ContentEntity.toContent(): Content? = runCatching { AppJson.decodeFromString(Content.serializer(), json) }.getOrNull()

/** [this] with [reading] over the fetched status, progress and times; unchanged when it is unknown or older than what [this] carries. */
fun Content.withReading(reading: EffectiveReading?) =
    if (reading == null || reading.seq < (userData?.readingSeq ?: 0)) this else copy(userData = reading.overlay(userData))
