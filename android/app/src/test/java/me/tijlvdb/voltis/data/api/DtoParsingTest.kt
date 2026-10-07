package me.tijlvdb.voltis.data.api

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.content.pageUrl
import me.tijlvdb.voltis.domain.comic.PageDimensions
import me.tijlvdb.voltis.domain.comic.pageDimensions
import me.tijlvdb.voltis.domain.reading.Envelope
import me.tijlvdb.voltis.domain.reading.Outcome
import me.tijlvdb.voltis.domain.reading.ReadingResult
import me.tijlvdb.voltis.domain.reading.ReadingState
import me.tijlvdb.voltis.domain.reading.SeriesInfo
import me.tijlvdb.voltis.domain.reading.SeriesReceipt
import me.tijlvdb.voltis.domain.reading.SeriesPrevious
import me.tijlvdb.voltis.domain.reading.Snapshot
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** Fixtures are the dev server's responses with made-up names and paths. */
class DtoParsingTest {
    private inline fun <reified T> fixture(name: String): T =
        AppJson.decodeFromString(checkNotNull(javaClass.getResource("/dto/$name.json")).readText())

    @Test
    fun comicWithPageSizesProgressAndIdentity() {
        val comic = fixture<Content>("comic")
        assertEquals(ContentType.COMIC, comic.type)
        assertEquals("c_8oZta1k1", comic.parentId)
        assertEquals(listOf(1.0, null), comic.orderParts)
        assertEquals("YesAndRightToLeft", comic.meta.manga)
        assertEquals(ContentLength("pages", 14, 9), comic.length)
        // A page without a size is 0, 0.
        assertEquals(
            listOf(PageDimensions(1000, 1422), PageDimensions(2000, 1422), PageDimensions(0, 0)),
            comic.fileData.pageDimensions(),
        )
        val user = checkNotNull(comic.userData)
        assertEquals("reading", user.status)
        assertNull(user.rating)
        assertEquals(JsonObject(mapOf("current_page" to JsonPrimitive(5), "progress_percent" to JsonPrimitive(35.7))), user.progress)

        // The `+` of the timestamp is encoded, or the server would read it as a space.
        assertEquals(
            "http://voltis.invalid/api/files/comic-page/c_1rrExFgA/2?v=2026-10-04T13%3A32%3A47.928428%2B04%3A00",
            pageUrl(comic, 2).toString(),
        )

        // A server without `uri` (before roadmap Phase 2), and one with the identity and user data.
        assertNull(comic.uri)
        val row = AppJson.decodeFromString<Content>(
            """{"id": "c_1", "title": "Vol. 1", "type": "comic", "library_id": "l_1", "uri": "comic/Quartz Lantern/v1", "file_size": 30103,
               "user_data": {"starred": false, "status": "reading", "progress": {}, "revision": "srv:a",
                             "status_updated_at": "2026-10-05T09:16:26+04:00", "progress_updated_at": null, "last_read_at": null}}""",
        )
        assertEquals(Triple("l_1", "comic/Quartz Lantern/v1", 30103L), Triple(row.libraryId, row.uri, row.fileSize))
        assertEquals("srv:a", row.userData?.revision)
        assertEquals("2026-10-05T09:16:26+04:00", row.userData?.statusUpdatedAt)

        // A series scanned by an older server has `"file_data": null`: the default, not a failure.
        val old = AppJson.decodeFromString<Content>("""{"id": "c_2", "title": "Series", "type": "comic_series", "file_data": null}""")
        assertEquals(FileData(), old.fileData)
    }

    @Test
    fun continueReading() {
        val (inSeries, standalone) = fixture<List<ContinueEntry>>("continue-reading")
        assertEquals("Plum Signal", inSeries.series?.title)
        assertEquals(5, inSeries.series?.unreadChildrenCount)
        assertEquals(JsonObject(emptyMap()), inSeries.series?.userData?.progress)
        // List rows come without pages.
        assertEquals(emptyList<PageDimensions>(), inSeries.item.fileData.pageDimensions())

        assertNull(standalone.series)
        assertEquals(true, standalone.isNew)
        assertNull(standalone.item.childrenCount)
    }

    @Test
    fun me() {
        val me = fixture<Me>("me")
        assertEquals(true, me.isAdmin)
        assertNull(me.email)
        assertEquals(LibraryVisibility.OVERFLOW, me.prefs.libraryVisibility("l_a1"))
        assertNull(me.prefs.libraryVisibility("l_b2"))
        assertNull(me.prefs.libraryVisibility("l_none"))

        // Stored preferences can be any JSON, and may be missing: both read as empty instead of failing the sign-in.
        for (preferences in listOf("", """, "preferences": [1]""", """, "preferences": null""")) {
            val bare = AppJson.decodeFromString<Me>("""{"id": "u_1", "username": "demo", "session_method": "password"$preferences}""")
            assertEquals(false, bare.isAdmin)
            assertNull(bare.prefs.libraryVisibility("l_a1"))
            assertEquals(250.0, bare.prefs.wordsPerMinute, 0.0)
        }
        val fast = AppJson.decodeFromString<Me>(
            """{"id": "u_1", "username": "demo", "session_method": "password", "preferences": {"reading": {"wordsPerMinute": 400}}}""",
        )
        assertEquals(400.0, fast.prefs.wordsPerMinute, 0.0)
        assertEquals(20.0, fast.prefs.secondsPerPage, 0.0)
    }

    @Test
    fun sessions() {
        val (app, browser) = fixture<List<Session>>("sessions")
        assertEquals(Triple("Tern Slate 7", SessionMethod.PASSWORD, true), Triple(app.clientName, app.method, app.current))
        assertEquals("2026-10-05T00:50:00.98389+04:00", app.createdAt)
        assertEquals(Triple(null, null, false), Triple(browser.clientName, browser.lastUsedAt, browser.current))
    }

    @Test
    fun seriesWithUserDataAndMetadata() {
        val series = fixture<Content>("series")
        assertEquals(ContentType.COMIC_SERIES, series.type)
        assertNull(series.parentId)
        assertEquals(3, series.unreadChildrenCount)
        assertEquals(
            UserData(
                starred = true, status = "reading", rating = 4, progress = JsonObject(emptyMap()),
                revision = "srv:id6q3qm2srifmnyorpooh326l2", statusUpdatedAt = "2026-10-04T15:13:32.075024+04:00",
            ),
            series.userData,
        )
        assertEquals(ContentLength("pages", 42, 42), series.length)
        assertEquals(listOf(StaffEntry("Odalys Venn", "writer"), StaffEntry("Tobin Marsh", "artist")), series.meta.staff)
        assertEquals(listOf(MetadataLink("Lowtide Press", "https://lowtide.example/gravel-choir")), series.meta.links)
        assertEquals("2019-03", series.meta.publicationDate)
        // Aligned with the metadata; a staff member without a page has a null key.
        assertEquals(
            FacetKeys(staff = listOf("odalys venn", null), genres = listOf("slice of life", "mystery"), tags = listOf("small town"), publishers = listOf("lowtide press")),
            series.facetKeys,
        )
    }

    @Test
    fun facets() {
        val page = fixture<FacetPage>("facet-page")
        assertEquals(7, page.total)
        assertEquals(Facet("science fiction", "science_fiction", 1), page.data[1])
        val person = fixture<FacetEntry>("facet-person")
        assertEquals(listOf(RoleCount("penciller", 1), RoleCount("writer", 1)), person.roles)
        // Other kinds have no roles; the field may be missing.
        assertEquals(emptyList<RoleCount>(), AppJson.decodeFromString<FacetEntry>("""{"key": "heist", "name": "heist", "count": 2}""").roles)
    }

    @Test
    fun customLists() {
        val (picks, empty) = fixture<List<CustomListSummary>>("custom-lists")
        assertEquals(CoverRef("c_CoPZdcYK", "dlw92keebu68"), picks.covers.first())
        assertNull(picks.description)
        assertEquals(ListVisibility.UNLISTED to 0, empty.visibility to empty.entryCount)
        assertEquals(emptyList<CoverRef>(), empty.covers)

        val detail = fixture<CustomListDetail>("custom-list")
        // The entry whose content row is gone is still listed and counted.
        assertEquals(2, detail.entryCount)
        val (gone, kept) = detail.entries
        assertNull(gone.content)
        assertEquals("l_AfKC8jmz" to "comic/Frost Thimble", gone.libraryId to gone.uri)
        assertEquals(ContentType.COMIC_SERIES, kept.content?.type)
        assertEquals("A made-up note", kept.notes)
        assertEquals("Made-up picks\nfor a rainy day", detail.description)
    }

    @Test
    fun continueTarget() {
        val next = fixture<ContinueTarget>("continue")
        assertEquals("Vol. 1", next.target?.title)
        assertEquals("c_fG5e8OEy", next.seriesId)
        assertEquals("start", next.action)
        assertNull(next.reason)
        assertNull(next.earlierUnreadId)
        // A caught-up series has neither a target nor an action.
        val caughtUp = AppJson.decodeFromString<ContinueTarget>(
            """{"target": null, "series_id": "c_1", "action": null, "reason": "caught_up", "is_new": false, "earlier_unread_id": null}""",
        )
        assertEquals(ContinueTarget(seriesId = "c_1", reason = ContinueReason.CAUGHT_UP), caughtUp)
    }

    @Test
    fun readingAnswers() {
        val envelope = fixture<Envelope>("reading-envelope")
        assertEquals("tq7k2m9x4c1v8b3n0:41", envelope.state.revision)
        assertEquals("tq7k2m9x4c1v8b3n0", envelope.writer)
        assertEquals(page2, envelope.state.progress)
        assertEquals(SeriesInfo("c_X4C2JtLo", "reading", "tq7k2m9x4c1v8b3n0:41", childrenCount = 3), envelope.series)

        // A held volume read on: its previous status for Undo, and the series it started.
        val write = fixture<ReadingResult>("reading-write")
        assertEquals(Outcome.MOVED_TO_READING, write.outcome)
        assertEquals(envelope, write.envelope)
        assertEquals(Snapshot("on_hold", JsonObject(emptyMap()), null), write.previous)
        assertEquals(SeriesPrevious("tq7k2m9x4c1v8b3n0:41", null, null), write.seriesPrevious)

        // A 409 carries the state the write lost to.
        assertEquals(envelope, fixture<Envelope>("reading-conflict"))

        // Untouched: no revision, empty progress; a series or a standalone item has no series.
        val blank = AppJson.decodeFromString<ReadingResult>(
            """{"state": {"revision": null, "status": null, "progress": {}}, "series": null, "writer": null, "outcome": "none"}""",
        )
        assertEquals(ReadingState(), blank.state)
        assertNull(blank.series)
    }

    /** `reading_seq` is a decimal string, absent for an untouched item; a series write answers with every state it left. */
    @Test
    fun readingSeqs() {
        assertEquals(9_007_199_254_740_993L, AppJson.decodeFromString<ReadingState>("""{"status": "reading", "reading_seq": "9007199254740993"}""").seq)
        assertEquals(0L, AppJson.decodeFromString<ReadingState>("""{"status": null}""").seq)
        assertEquals(7L, AppJson.decodeFromString<UserData>("""{"status": "reading", "reading_seq": "7"}""").readingSeq)
        val receipt = AppJson.decodeFromString<SeriesReceipt>(
            """{"count": 2, "series": {"id": "s", "status": "reading", "reading_seq": "12"}, "items": [{"id": "a", "status": "completed", "progress": {}, "reading_seq": "11"}]}""",
        )
        assertEquals(listOf("s" to 12L, "a" to 11L), receipt.snapshots.map { it.contentId to it.state.seq })
    }

    private val page2 = JsonObject(mapOf("current_page" to JsonPrimitive(2), "progress_percent" to JsonPrimitive(33.3)))
}
