package me.tijlvdb.voltis.domain.catalog

import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.LibraryVisibility
import org.junit.Assert.assertEquals
import org.junit.Test

class VisibilityEditsTest {
    /** Only the edited libraries are named, and Show goes out as a null: `explicitNulls = false` leaves a JsonObject's nulls alone. */
    @Test
    fun visibilityEditsAndPatch() {
        assertEquals(
            """{"libraries":{"l_a1":{"visibility":null},"l_b2":{"visibility":"hide"}}}""",
            // Encoded as the request's converter does.
            AppJson.encodeToString(VisibilityEdits.patch(mapOf("l_a1" to LibraryVisibility.SHOW, "l_b2" to LibraryVisibility.HIDE))),
        )

        // Stored Show, Hide chosen and submitted, Show chosen again before the answer: that choice outlives the save.
        val sent = VisibilityEdits.choose(emptyMap(), "l_a1", LibraryVisibility.HIDE, stored = LibraryVisibility.SHOW, sent = null)
        val during = VisibilityEdits.choose(sent, "l_a1", LibraryVisibility.SHOW, stored = LibraryVisibility.SHOW, sent = sent)
        assertEquals(mapOf("l_a1" to LibraryVisibility.SHOW), VisibilityEdits.unsaved(during, sent))
        // With no save out, choosing what is stored is no edit, and what was submitted is none once stored.
        assertEquals(emptyMap<String, String>(), VisibilityEdits.choose(sent, "l_a1", LibraryVisibility.SHOW, LibraryVisibility.SHOW, sent = null))
        assertEquals(emptyMap<String, String>(), VisibilityEdits.unsaved(sent, sent))
    }
}
