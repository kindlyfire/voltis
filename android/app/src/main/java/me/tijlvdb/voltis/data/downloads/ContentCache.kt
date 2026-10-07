package me.tijlvdb.voltis.data.downloads

import me.tijlvdb.voltis.data.storage.localTransaction
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.FileData
import me.tijlvdb.voltis.data.db.ContentEntity
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.data.db.importReading
import me.tijlvdb.voltis.domain.reading.snapshotOf

fun Content.toEntity(position: Int?, detail: Boolean, fetchedAt: Long) = ContentEntity(
    id, libraryId, uri, parentId, type, title, position, valid, fileMtime, fileSize, coverVersion,
    AppJson.encodeToString(Content.serializer(), this), detail, fetchedAt,
)

/** A row from `GET /content/:id`, which has the metadata and pages a list row leaves empty. */
val Content.isDetail get() = length != null || fileData.pages != null

/**
 * What the download path seeds a lane with: a comic without its pages and length, so a lane's
 * count of a downloaded item comes only from its copy and the reader, never from a seed that
 * raced the copy's publication.
 */
fun Content.forSeed(): Content = if (type == ContentType.COMIC) copy(fileData = FileData(), length = null) else this

/**
 * Caches [rows] with their positions, each a detail row when [detail] says so: a list row updates
 * a detail row's columns but never its `json`, and a row without a position keeps the one it had.
 * Each chunk is read, merged and written in one transaction, so a detail row stored meanwhile is kept.
 * This is metadata only: each row's reading state goes to the snapshot table, through the import
 * primitive, in the same transaction ([Content.userData] null is the untouched state, seq 0).
 */
suspend fun VoltisDatabase.cache(rows: List<Pair<Content, Int?>>, fetchedAt: Long, detail: (Content) -> Boolean) {
    val dao = content()
    // Below SQLite's oldest limit on bound parameters (999).
    for (chunk in rows.chunked(500)) {
        localTransaction {
            val existing = dao.get(chunk.map { it.first.id }).associateBy { it.id }
            importReading(chunk.map { (content, _) -> snapshotOf(content.id, content.userData) })
            dao.upsert(
                chunk.map { (content, position) ->
                    val isDetail = detail(content)
                    val new = content.toEntity(position, isDetail, fetchedAt)
                    val old = existing[content.id] ?: return@map new
                    new.copy(
                        position = position ?: old.position, json = if (old.detail && !isDetail) old.json else new.json, detail = old.detail || isDetail,
                        volumesKnown = old.volumesKnown,
                    )
                },
            )
        }
    }
}

/**
 * Rewrites `user_data.starred` and `user_data.rating` in a cached row's `json` (P2 Addendum 2): a
 * null argument leaves the field, `JsonNull` clears the rating. Without a cached row nothing happens.
 * Not a transaction of its own: `RoomPendingStore` calls it inside the one that lands a write.
 */
suspend fun VoltisDatabase.patchUserData(contentId: String, starred: Boolean?, rating: JsonElement?) {
    val row = content().get(contentId) ?: return
    val json = AppJson.parseToJsonElement(row.json).jsonObject
    val user = (json["user_data"] as? JsonObject).orEmpty().toMutableMap()
    starred?.let { user["starred"] = JsonPrimitive(it) }
    rating?.let { user["rating"] = it }
    content().setJson(contentId, JsonObject(json + ("user_data" to JsonObject(user))).toString())
}
