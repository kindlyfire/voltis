package me.tijlvdb.voltis.data.reading

import androidx.room.withTransaction
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import me.tijlvdb.voltis.data.content.toContent
import me.tijlvdb.voltis.data.db.ContentEntity
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.data.db.toState
import me.tijlvdb.voltis.domain.reading.EffectiveReading
import me.tijlvdb.voltis.domain.reading.Lane
import me.tijlvdb.voltis.domain.reading.guardVolumes
import me.tijlvdb.voltis.domain.reading.pageCount
import me.tijlvdb.voltis.domain.reading.references
import me.tijlvdb.voltis.domain.reading.effectiveReading

/**
 * The one effective-state read: [ids]' snapshots with the outbox's unsent ops applied in order, from a single
 * transaction. Offline pages, offline Continue and the automatic downloads' eligibility all read it.
 */
suspend fun VoltisDatabase.effectiveReading(ids: Collection<String>): Map<String, EffectiveReading> = withTransaction {
    val ops = ops().all().map { it.toOp() }
    // Everything an op reads or writes, as well as what was asked for.
    val needed = (ids + ops.flatMap { listOf(it.contentId) + it.guard?.guardVolumes().orEmpty() }).toSet()
    val laneRows = needed.chunked(500).flatMap { lanes().get(it) }.associate { it.contentId to it.toLane() }
    val all = needed + ops.flatMap { it.references(laneRows[it.contentId]?.parentId) }
    val states = all.toList().chunked(500).flatMap { snapshots().get(it) }.associate { it.contentId to it.toState() }
    // A volume without a lane still has a row to tell its type, series and page count by.
    val missing = needed.filter { it !in laneRows }
    val rows = missing.chunked(500).flatMap { content().get(it) }.associate { it.id to it.toLane() }
    effectiveReading(ids, states, laneRows + rows, ops)
}

private fun ContentEntity.toLane() = Lane(id, libraryId, uri, parentId, type, title, toContent()?.pageCount())

/** [effectiveReading] again whenever a snapshot, a lane, an op or a content row (a fallback's type and page count) changes. */
fun VoltisDatabase.effectiveReadings(ids: Collection<String>): Flow<Map<String, EffectiveReading>> =
    invalidationTracker.createFlow("reading_snapshot", "reading_lane", "reading_op", "content").map { effectiveReading(ids) }
