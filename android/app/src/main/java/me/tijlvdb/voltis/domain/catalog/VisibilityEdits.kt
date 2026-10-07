package me.tijlvdb.voltis.domain.catalog

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.LibraryVisibility

/** The unsaved choices of the Library visibility screen: library ID to one of [LibraryVisibility]. */
object VisibilityEdits {
    /**
     * The unsaved choices after choosing [visibility] for a library. Choosing what is [stored]
     * drops the edit, unless a save of that library is out ([sent]): its outcome isn't stored yet.
     */
    fun choose(edits: Map<String, String>, libraryId: String, visibility: String, stored: String, sent: Map<String, String>?) =
        if (visibility == stored && sent?.containsKey(libraryId) != true) edits - libraryId else edits + (libraryId to visibility)

    /** What is left unsaved once [sent] is stored: the choices made during the request that differ from it. */
    fun unsaved(edits: Map<String, String>, sent: Map<String, String>) = edits.filter { (id, visibility) -> sent[id] != visibility }

    /**
     * The merge patch that saves [edits]. It names only the edited libraries and only their
     * `visibility`, so what changed elsewhere since the last fetch stays. Show is stored as
     * absent: an explicit null, which a merge patch needs to remove a member.
     */
    fun patch(edits: Map<String, String>): JsonObject {
        val libraries = edits.mapValues { (_, value) ->
            JsonObject(mapOf("visibility" to JsonPrimitive(value.takeIf { it != LibraryVisibility.SHOW })))
        }
        return JsonObject(mapOf("libraries" to JsonObject(libraries)))
    }
}
