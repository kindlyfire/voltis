package me.tijlvdb.voltis.data.lists

import me.tijlvdb.voltis.data.storage.localTransaction
import kotlinx.serialization.builtins.ListSerializer
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.CoverRef
import me.tijlvdb.voltis.data.api.CustomListDetail
import me.tijlvdb.voltis.data.api.CustomListSummary
import me.tijlvdb.voltis.data.db.CustomListEntity
import me.tijlvdb.voltis.data.db.CustomListEntryEntity
import me.tijlvdb.voltis.data.db.VoltisDatabase

// The lists cache's transactions (P4 §7).

private val coverList = ListSerializer(CoverRef.serializer())

/**
 * The index: each list updated or inserted in the server's order, keeping when its entries were
 * fetched. A list whose count or covers differ from the cache had its entries changed by a scan
 * without `updated_at` moving: its detail fetch time is cleared, so it is fetched until one lands.
 * Returns the cached lists the index no longer has, for the caller to delete.
 */
suspend fun VoltisDatabase.storeIndex(index: List<CustomListSummary>, fetchedAt: Long): List<String> = localTransaction {
    val dao = lists()
    val old = dao.all().associateBy { it.id }
    val counts = dao.entryCounts().associate { it.listId to it.n }
    dao.upsert(
        index.mapIndexed { i, list ->
            val cached = old[list.id]
            val changed = cached == null || (counts[list.id] ?: 0) != (list.entryCount ?: 0) || cached.coverRefs() != list.covers
            CustomListEntity(
                list.id, i, list.name, list.description, list.visibility, list.entryCount ?: 0, list.updatedAt,
                AppJson.encodeToString(coverList, list.covers), cached?.entriesUpdatedAt, fetchedAt,
                if (changed) null else cached.detailFetchedAt, cached?.revision ?: 0,
            )
        },
    )
    (old.keys - index.mapTo(HashSet()) { it.id }).toList()
}

/** A list's detail: its fields, and every entry row replaced. Covers stay the index's. */
suspend fun VoltisDatabase.storeList(detail: CustomListDetail, fetchedAt: Long) = localTransaction {
    val dao = lists()
    val old = dao.get(detail.id)
    dao.upsert(
        listOf(
            CustomListEntity(
                detail.id, old?.position ?: -1, detail.name, detail.description, detail.visibility,
                detail.entryCount ?: detail.entries.size, detail.updatedAt, old?.covers ?: "[]", detail.updatedAt, fetchedAt, fetchedAt,
                (old?.revision ?: 0) + 1,
            ),
        ),
    )
    dao.deleteEntries(listOf(detail.id))
    dao.insertEntries(
        detail.entries.mapIndexed { i, entry ->
            val content = entry.content
            CustomListEntryEntity(
                detail.id, entry.libraryId, entry.uri, entry.id, i, entry.notes,
                content?.id, content?.title ?: entry.uri, content?.type, content?.coverVersion,
            )
        },
    )
}

suspend fun VoltisDatabase.deleteLists(ids: List<String>) = localTransaction {
    lists().deleteEntries(ids)
    lists().delete(ids)
}

fun CustomListEntity.coverRefs(): List<CoverRef> = runCatching { AppJson.decodeFromString(coverList, covers) }.getOrDefault(emptyList())
