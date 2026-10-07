package me.tijlvdb.voltis.domain.catalog

import java.time.ZoneId
import java.util.Locale
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentLength
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ContinueReason
import me.tijlvdb.voltis.data.api.ContinueTarget
import me.tijlvdb.voltis.data.api.DisplayMetadata
import me.tijlvdb.voltis.data.api.FacetKeys
import me.tijlvdb.voltis.data.api.FileData
import me.tijlvdb.voltis.data.api.MetadataLink
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.data.api.StaffEntry
import me.tijlvdb.voltis.data.api.UserData
import org.junit.Assert.assertEquals
import org.junit.Test

class CatalogTextTest {
    private fun series(
        title: String = "Ember Saga",
        type: String = ContentType.BOOK_SERIES,
        kind: String? = null,
        altTitles: List<String>? = null,
    ) = Content("s", title, type, meta = DisplayMetadata(kind = kind, altTitles = altTitles))

    private fun item(title: String, vararg parts: Double?) = Content("c", title, "book", orderParts = parts.toList())

    private val labels = ItemLabels("Volume %s", "Chapter %s", "Vol. %s", "Ch. %s", "#%s")

    private fun harbor(kind: String? = null) = series("Harbor Lights", ContentType.COMIC_SERIES, kind)

    /** The cases of `utils/seriesItem.test.ts`. */
    @Test
    fun seriesItems() {
        data class Case(
            val title: String,
            val label: String?,
            val series: Content,
            val parts: List<Double?>,
            val stripped: String?,
            val removedNumber: Boolean,
        )
        val saga = series()
        val cases = listOf(
            Case("Ember Saga, Vol. 01", "Volume 1", saga, listOf(1.0), null, true),
            Case("Ember Saga, Vol. 3: The Long Road", "Volume 3", saga, listOf(3.0), "The Long Road", true),
            Case("Ember Saga, Vol. 3 – The Long Road", "Volume 3", saga, listOf(3.0), "The Long Road", true),
            Case("Ember Saga, Vol. 1-3", "Volume 1", saga, listOf(1.0), null, true),
            Case("Ember Saga v 2", "Volume 2", saga, listOf(2.0), null, true),
            Case("Ember Saga 3", "Volume 3", saga, listOf(3.0), null, true),
            Case("Ember Saga 3: The Long Road", "Volume 3", saga, listOf(3.0), "The Long Road", true),
            Case("Ember Saga: 7 Days", "Volume 1", saga, listOf(1.0), "7 Days", false),
            Case("Ember Saga, Vol. 2: 7 Days", "Volume 2", saga, listOf(2.0), "7 Days", true),
            Case("4096", "Volume 3", saga, listOf(3.0), null, false),
            Case("v3", "Volume 3", saga, listOf(3.0), null, true),
            Case("Ember Saga, Vol. 3.5", "Volume 3.5", saga, listOf(3.5), null, true),
            Case("Ember Saga: Extras", null, saga, listOf(-1.0), "Extras", false),
            Case("Ember Saga, Vol. 3: The Long Road", null, saga, listOf(null), "Vol. 3: The Long Road", false),
            Case("EMBER  SAGA – The Long Road", null, saga, listOf(null), "The Long Road", false),
            Case(
                "Saga of Embers, Vol. 3: The Long Road",
                "Volume 3",
                series(altTitles = listOf("Saga of Embers")),
                listOf(3.0),
                "The Long Road",
                true,
            ),
            Case("Embers of Dusk", null, series("Ember"), listOf(null), "Embers of Dusk", false),
            Case("The Tinker's Road: Short Stories", null, series("The Tinker’s Road"), listOf(100000.0), "Short Stories", false),
            Case("The Return", "Chapter 12", harbor("manga"), listOf(null, 12.0), "The Return", false),
            Case("Vol. 2 Ch. 5", "Vol. 2 · Ch. 5", harbor("manga"), listOf(2.0, 5.0), null, true),
            Case("Vol. 2 Ch. 5", "Vol. 2 · #5", harbor("comic"), listOf(2.0, 5.0), null, true),
            Case("#12", "#12", harbor("comic"), listOf(null, 12.0), null, true),
            Case("Vol. 3", "Volume 3", harbor(), listOf(3.0, null), null, true),
            // An ideographic space and a no-break space separate like any other, as `\s` does on the web.
            Case("Ember Saga\u3000Vol.\u00A03: The Long Road", "Volume 3", saga, listOf(3.0), "The Long Road", true),
        )
        for (case in cases) {
            assertEquals(
                case.title,
                SplitTitle(case.label, case.stripped, case.removedNumber),
                splitItemTitle(item(case.title, *case.parts.toTypedArray()), case.series, labels),
            )
        }
    }

    @Test
    fun itemAndReaderTitles() {
        val saga = series()
        assertEquals("Volume 3: The Long Road", itemName(item("Ember Saga, Vol. 3: The Long Road", 3.0), saga, labels))
        assertEquals("Volume 1", itemName(item("Ember Saga, Vol. 01", 1.0), saga, labels))
        assertEquals("Extras", itemName(item("Ember Saga: Extras", -1.0), saga, labels))
        assertEquals("Ember Saga", itemName(item("Ember Saga", null), saga, labels))
        assertEquals("Loose Leaf", itemName(item("Loose Leaf"), null, labels))

        assertEquals("Harbor Lights · Volume 3", readerTitle(item("Vol. 3", 3.0, null), harbor(), labels))
        // The item title alone when the series adds nothing, or there is none.
        assertEquals("Ember Saga", readerTitle(item("Ember Saga", null), saga, labels))
        assertEquals("Loose Leaf", readerTitle(item("Loose Leaf"), null, labels))

        // A page's eyebrow: the number, unless the title is nothing but series name and numbering.
        assertEquals("Volume 3", itemEyebrow(item("Ember Saga, Vol. 3: The Long Road", 3.0), saga, labels))
        assertEquals(null, itemEyebrow(item("Ember Saga, Vol. 01", 1.0), saga, labels))
        assertEquals("Volume 3", itemEyebrow(item("4096", 3.0), saga, labels))
    }

    private val progressLabels = ProgressLabels("Page %d / %d", "%d%%", "%d/%d read", "%s · %d dropped")

    private fun progress(page: Int? = null, percent: Double? = null) = JsonObject(
        buildMap {
            page?.let { put("current_page", JsonPrimitive(it)) }
            percent?.let { put("progress_percent", JsonPrimitive(it)) }
        },
    )

    /** `pages` null mirrors a list row, which comes without `file_data`. */
    private fun comic(progress: JsonObject, pages: Int? = null, status: String = ReadingStatus.READING) = Content(
        "c",
        "Loose Leaf",
        ContentType.COMIC,
        fileData = FileData(pages?.let { n -> List(n) { JsonArray(listOf(JsonPrimitive("$it.jpg"))) } }),
        userData = UserData(status = status, progress = progress),
    )

    private fun book(percent: Double?, status: String = ReadingStatus.READING) =
        Content("b", "Loose Leaf", ContentType.BOOK, userData = UserData(status = status, progress = progress(percent = percent)))

    private fun counted(total: Int, unread: Int, status: String? = null, dropped: Int = 0, added: Int? = null) = harbor().copy(
        childrenCount = total,
        unreadChildrenCount = unread,
        completedChildrenCount = total - unread - dropped,
        droppedChildrenCount = dropped,
        newChildrenCount = added,
        userData = status?.let { UserData(status = it, progress = JsonObject(emptyMap())) },
    )

    /** The cases of `utils/contentProgress.test.ts`. */
    @Test
    fun contentProgress() {
        val cases = listOf(
            Triple("comic saved on its first page", comic(progress(0, 0.0), 140), ContentProgress(0f, "Page 1 / 140")),
            Triple("comic one-page, read", comic(progress(0), 1), ContentProgress(0f, "Page 1 / 1")),
            Triple("comic first page, no file_data", comic(progress(0, 0.0)), ContentProgress(0f, "0%")),
            Triple("comic without current_page", comic(progress(), 140), null),
            Triple("comic pre-change progress, no file_data", comic(progress(12)), null),
            Triple("comic mid-read", comic(progress(12, 50.0), 140), ContentProgress(12f / 140, "Page 13 / 140")),
            Triple("comic mid-read, no file_data", comic(progress(12, 9.3)), ContentProgress(0.093f, "9%")),
            Triple("comic on the last page", comic(progress(139), 140), ContentProgress(139f / 140, "Page 140 / 140")),
            Triple("comic completed", comic(progress(12), 140, ReadingStatus.COMPLETED), null),
            Triple("comic saved past a rescan that removed pages", comic(progress(40), 10), ContentProgress(0.9f, "Page 10 / 10")),
            Triple("book saved at its start", book(0.0), ContentProgress(0f, "0%")),
            Triple("book without a percent", book(null), null),
            Triple("book barely started", book(0.4), ContentProgress(0.004f, "1%")),
            Triple("book nearly done", book(99.6), ContentProgress(0.996f, "99%")),
            Triple("book at its end, not completed", book(100.0), ContentProgress(1f, "100%")),
            Triple("book dropped", book(40.0, ReadingStatus.DROPPED), null),
            Triple("series partially read", counted(10, 4), ContentProgress(0.6f, "6/10 read")),
            Triple("series partially read and dropped", counted(12, 1, dropped = 1), ContentProgress(11f / 12, "10/12 read · 1 dropped")),
            Triple("series with nothing read", counted(10, 10), null),
            Triple("series fully read", counted(10, 0), null),
            Triple("series completed", counted(10, 4, ReadingStatus.COMPLETED), null),
            Triple("series without children", harbor(), null),
        )
        for ((name, content, expected) in cases) {
            val result = contentProgress(content, progressLabels)
            assertEquals(name, expected?.label, result?.label)
            assertEquals(name, expected?.fraction ?: -1f, result?.fraction ?: -1f, 1e-6f)
        }
    }

    /** What a card shows and how TalkBack names it, as `linkLabel` in `ContentGrid/Item.vue`. */
    @Test
    fun cards() {
        val cardLabels = CardLabels(
            labels,
            progressLabels,
            read = "Read %s",
            new = "New",
            newCount = "%d new",
            caughtUp = "%s · Caught up",
            unread = "%d unread",
            items = "%d items",
            statuses = mapOf(ReadingStatus.READING to "Reading", ReadingStatus.COMPLETED to "Completed"),
        )
        val saga = series().copy(childrenCount = 4, completedChildrenCount = 2)
        val chapter = item("The Return", null, 12.0).copy(userData = UserData(status = ReadingStatus.READING))
        val odd = comic(progress(), status = "paused").copy(unreadChildrenCount = 3)

        data class Case(
            val name: String,
            val content: Content,
            val series: Content?,
            val read: Boolean,
            val isNew: Boolean,
            val expected: CardText,
        )
        val cases = listOf(
            Case(
                "a read card is titled by its series; its title holds the number",
                item("Ember Saga, Vol. 3: The Long Road", 3.0), saga, read = true, isNew = true,
                CardText("Ember Saga", "Volume 3 · 2/4 read", "New", null, null, "Read Ember Saga, Ember Saga, Vol. 3: The Long Road, 2/4 read, New"),
            ),
            Case(
                "the label is said when the title lost no number",
                chapter, harbor("manga"), read = true, isNew = false,
                CardText("Harbor Lights", "Chapter 12", null, "Reading", null, "Read Harbor Lights, Chapter 12, The Return, Reading"),
            ),
            Case(
                "a completed series with unread volumes added since",
                counted(5, 2, ReadingStatus.COMPLETED, added = 2), null, read = false, isNew = false,
                CardText("Harbor Lights", null, "2 new", "Completed", 2, "Harbor Lights, 2 new, Completed, 2 unread"),
            ),
            Case(
                "a caught-up series hides its zero count",
                counted(5, 0, ReadingStatus.READING, dropped = 1), null, read = false, isNew = false,
                CardText("Harbor Lights", null, null, "Reading · Caught up", null, "Harbor Lights, Reading · Caught up"),
            ),
            Case(
                "an item's count and an unknown status",
                odd, null, read = false, isNew = false,
                CardText("Loose Leaf", null, null, "paused", null, "Loose Leaf, paused"),
            ),
        )
        for (case in cases) {
            assertEquals(case.name, case.expected, cardText(case.content, case.series, case.read, case.isNew, cardLabels))
        }
        // An item of its series: its number over what is left of its title, or its number alone.
        val named = cardText(item("Ember Saga, Vol. 3: The Long Road", 3.0), null, read = true, isNew = false, cardLabels, parent = saga)
        assertEquals(CardText("Volume 3", "The Long Road", null, null, null, "Read Ember Saga, Vol. 3: The Long Road"), named)
        val numbered = cardText(chapter, null, read = true, isNew = false, cardLabels, parent = harbor("manga"))
        assertEquals(CardText("Chapter 12", "The Return", null, "Reading", null, "Read Chapter 12, The Return, Reading"), numbered)
        assertEquals("Volume 1", cardText(item("Ember Saga, Vol. 01", 1.0), null, read = true, isNew = false, cardLabels, parent = saga).title)

        // The total count shows a caught-up series' children too.
        val total = cardText(counted(5, 0, ReadingStatus.READING, dropped = 1), null, read = false, isNew = false, cardLabels, ItemCountMode.Total)
        assertEquals(5, total.count)
        assertEquals("Harbor Lights, Reading · Caught up, 5 items", total.linkLabel)
    }

    /** The labels of `ContinueReadingButton.vue`. */
    @Test
    fun continueLabels() {
        val saga = series()
        val held = item("Ember Saga, Vol. 3: The Long Road", 3.0)
        val cases = listOf(
            ContinueTarget(target = held, action = "start") to ContinueLabel.Start,
            ContinueTarget(target = held, action = "resume") to ContinueLabel.Continue,
            ContinueTarget(target = held, action = "next") to ContinueLabel.Next,
            ContinueTarget(target = held, action = "resume", reason = ContinueReason.HELD) to ContinueLabel.Resume("Volume 3: The Long Road"),
            ContinueTarget(reason = ContinueReason.EARLIER_UNREAD, earlierUnreadId = "c_1") to ContinueLabel.Earlier,
            ContinueTarget(reason = ContinueReason.COMPLETED) to ContinueLabel.Again,
            ContinueTarget(reason = ContinueReason.CAUGHT_UP) to ContinueLabel.Again,
            ContinueTarget(reason = ContinueReason.EMPTY) to ContinueLabel.Empty,
            // A target without an action starts.
            ContinueTarget(target = held) to ContinueLabel.Start,
        )
        for ((target, expected) in cases) assertEquals(target.toString(), expected, continueLabel(target, saga, labels))
    }

    /** The cases of `utils/readingTime.test.ts`, and the compact counts `Intl.NumberFormat` gives. */
    @Test
    fun readingTime() {
        val length = LengthLabels("%s words", "1 page", "%s pages", "< 1 min", "%d min", "%d h", "%d h %d min", "%s · %s", "%s · %s left")
        for ((minutes, expected) in listOf(0.4 to "< 1 min", 45.0 to "45 min", 59.7 to "1 h", 62.0 to "1 h", 330.0 to "5 h 30 min", 725.0 to "12 h")) {
            assertEquals(expected, formatDuration(minutes, length))
        }
        for ((n, expected) in listOf(950 to "950", 1000 to "1K", 12_345 to "12.3K", 82_000 to "82K", 999_950 to "1M", 1_250_000 to "1.3M")) {
            assertEquals(expected, compactNumber(n))
        }
        val cases = listOf(
            Triple("words unread", ContentLength("words", 82_000, 82_000), "82K words · 5 h 30 min"),
            Triple("words in progress", ContentLength("words", 82_000, 47_500), "82K words · 3 h 10 min left"),
            Triple("grouped pages", ContentLength("pages", 1212, 1212), "1,212 pages · 6 h 45 min"),
            Triple("finished", ContentLength("pages", 212, 0), "212 pages · 1 h 10 min"),
            Triple("one page", ContentLength("pages", 1, 1), "1 page · < 1 min"),
        )
        for ((name, value, expected) in cases) assertEquals(name, expected, lengthSummary(value, 250.0, 20.0, length))
    }

    /** The rows of `details` in `InfoHeader.vue`, and the cover's progress of `CoverProgress.vue`. */
    @Test
    fun detailsAndCoverProgress() {
        val meta = DisplayMetadata(
            staff = listOf(StaffEntry("Odalys Venn", "writer"), StaffEntry("Tobin Marsh", "artist")),
            publishers = emptyList(),
            publicationDate = "2019-03-07",
            genres = listOf("slice_of_life", "mystery"),
            // Only web links are kept: a browser can't be handed another scheme.
            links = listOf(MetadataLink("Lowtide Press", "https://lowtide.example"), MetadataLink("Scan", "file:///library/scan.pdf")),
        )
        val staff = listOf(DetailItem("Odalys Venn", " (writer)"), DetailItem("Tobin Marsh", " (artist)"))
        val genres = listOf(DetailItem("Slice of life"), DetailItem("Mystery"))
        assertEquals(
            listOf(
                DetailRow(DetailField.Staff, "Odalys Venn (writer), Tobin Marsh (artist)", staff),
                DetailRow(DetailField.Published, "March 7, 2019"),
                DetailRow(DetailField.Length, "42 pages"),
                DetailRow(DetailField.Genres, "Slice of life, Mystery", genres),
                DetailRow(DetailField.Links, links = meta.links!!.take(1)),
            ),
            detailRows(harbor().copy(meta = meta), "42 pages", Locale.US),
        )
        // With the keys a series has, a value links to its Discover page; one with a null key doesn't.
        val keys = FacetKeys(staff = listOf("odalys venn", null), genres = listOf("slice of life", "mystery"))
        val linked = detailRows(harbor().copy(meta = meta, facetKeys = keys), null, Locale.US)
        assertEquals(
            listOf(FacetRef(FacetKind.PEOPLE, "odalys venn"), null, FacetRef(FacetKind.GENRES, "slice of life"), FacetRef(FacetKind.GENRES, "mystery")),
            linked.flatMap { row -> row.items.map { it.facet } },
        )
        assertEquals(emptyList<DetailRow>(), detailRows(harbor(), null))
        // A year or a year-month has no day to show; a timestamp shows its UTC day.
        assertEquals("2019-03", formatDate("2019-03", Locale.US))
        assertEquals("March 6, 2019", formatDate("2019-03-07T01:00:00+04:00", Locale.US))
        assertEquals("soon", formatDate("soon", Locale.US))
        // A moment's date is the device's: the same instant is the 7th in Dubai and the 6th in UTC.
        assertEquals("March 7, 2019", formatTimestampDate("2019-03-07T01:00:00+04:00", ZoneId.of("Asia/Dubai"), Locale.US))
        assertEquals("March 6, 2019", formatTimestampDate("2019-03-07T01:00:00+04:00", ZoneId.of("UTC"), Locale.US))

        // Unlike a card's, a series' bar stays when all is read, and whatever the status.
        val caughtUp = "%s · Caught up"
        assertEquals("5/5 read · Caught up", coverProgress(counted(5, 0, ReadingStatus.READING), progressLabels, caughtUp)?.label)
        assertEquals("5/5 read", coverProgress(counted(5, 0, ReadingStatus.COMPLETED), progressLabels, caughtUp)?.label)
        assertEquals(0.6f, coverProgress(counted(10, 4, ReadingStatus.DROPPED), progressLabels, caughtUp)!!.fraction, 1e-6f)
        assertEquals(null, coverProgress(counted(10, 10), progressLabels, caughtUp))
        assertEquals("Page 13 / 140", coverProgress(comic(progress(12, 50.0), 140), progressLabels, caughtUp)?.label)
    }
}
