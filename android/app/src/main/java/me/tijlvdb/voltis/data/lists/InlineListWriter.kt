package me.tijlvdb.voltis.data.lists

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.CustomListSummary
import me.tijlvdb.voltis.data.api.ForAccount
import me.tijlvdb.voltis.data.api.VoltisApi

/**
 * The direct list requests (P4 §9, decision 14), for one account: each is tagged with it, and an
 * error response is an [HttpFailure]. Bodies are JsonObjects so a null is sent.
 */
internal class InlineListWriter(private val api: VoltisApi, account: String) {
    private val tag = ForAccount(account)

    suspend fun create(name: String, description: String?, visibility: String): CustomListSummary =
        call { api.createCustomList(listBody(name, description, visibility), tag) }

    suspend fun update(id: String, name: String, description: String?, visibility: String) =
        call { api.updateCustomList(id, listBody(name, description, visibility), tag) }

    suspend fun delete(id: String) = call { api.deleteCustomList(id, tag) }

    /** The server's count of entries added. */
    suspend fun add(listIds: List<String>, contentIds: List<String>): Int =
        call { api.addListEntries(JsonObject(mapOf("list_ids" to strings(listIds), "ids" to strings(contentIds))), tag) }.count

    suspend fun reorder(listId: String, entryIds: List<String>) =
        call { api.reorderListEntries(listId, JsonObject(mapOf("ctc_ids" to strings(entryIds))), tag) }

    suspend fun setNotes(listId: String, entryId: String, notes: String?) =
        call { api.updateListEntry(listId, entryId, JsonObject(mapOf("notes" to JsonPrimitive(notes))), tag) }

    suspend fun remove(listId: String, entryId: String) = call { api.deleteListEntry(listId, entryId, tag) }

    private fun listBody(name: String, description: String?, visibility: String) = JsonObject(
        mapOf("name" to JsonPrimitive(name), "description" to JsonPrimitive(description), "visibility" to JsonPrimitive(visibility)),
    )

    private fun strings(values: List<String>) = JsonArray(values.map(::JsonPrimitive))
}
