package me.tijlvdb.voltis.data.content

import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.DisplayMetadata
import me.tijlvdb.voltis.data.api.UserData
import org.junit.Assert.assertEquals
import org.junit.Test

class ContentMemoTest {
    private var live: Any? = "store-a"
    private val memo = ContentMemo({ live }, maxRows = 400, maxListRows = 30, maxListSize = 20, maxLists = 8)

    private fun row(id: String, parent: String? = null, description: String? = null) =
        Content(id, "Title $id", ContentType.COMIC_SERIES, parentId = parent, meta = DisplayMetadata(description = description))

    private fun volumes(series: String) = ContentListParams.volumes(series)

    /** Another account's store is a new generation: nothing of the old one is read, and an answer in flight for it writes nothing. */
    @Test
    fun accountGenerationIsolates() {
        val a = live
        memo.put(a, listOf(row("c1")), detail = true)
        assertEquals("Title c1", memo.get(a, "c1")?.title)

        live = "store-b"
        assertEquals(null, memo.get(live, "c1"))
        // A's response, arriving after the switch, is rejected; B's own is kept.
        memo.put(a, listOf(row("c2")), detail = true)
        memo.put(live, listOf(row("c3")), detail = true)
        assertEquals(listOf(null, "Title c3", null), listOf(memo.get(live, "c2"), memo.get(live, "c3")?.title, memo.get(a, "c3")))

        // No open store (opening, failed, signed out): nothing is kept or read.
        live = null
        memo.put(null, listOf(row("c4")), detail = true)
        live = "store-b"
        assertEquals(null, memo.get(live, "c4"))
    }

    /** A list row keeps the detail-only fields of a known detail row; a detail response replaces them. */
    @Test
    fun listRowKeepsDetailFields() {
        val a = live
        memo.put(a, listOf(row("c1", description = "Long text")), detail = true)
        memo.put(a, listOf(row("c1").copy(title = "Renamed")), detail = false)
        assertEquals(listOf("Renamed", "Long text"), memo.get(a, "c1")!!.let { listOf(it.title, it.meta.description) })
        memo.put(a, listOf(row("c1")), detail = true)
        assertEquals(null, memo.get(a, "c1")?.meta?.description)
    }

    /** Forgetting an item drops its row, the lists that hold it and the list of its children. */
    @Test
    fun forgetEvicts() {
        val a = live
        memo.put(a, listOf(row("c1"), row("v1", "c1"), row("o1")), detail = false)
        memo.putList(a, volumes("c1"), listOf(row("v1", "c1")))
        memo.putList(a, volumes("c2"), listOf(row("v2", "c2"), row("c1")))
        memo.putList(a, volumes("c3"), listOf(row("v3", "c3")))
        memo.forget(a, "c1")
        assertEquals(listOf(null, null, null, true), listOf(memo.get(a, "c1"), memo.list(a, volumes("c1")), memo.list(a, volumes("c2")), memo.list(a, volumes("c3")) != null))
    }

    /** Lists are bounded in total: an oversized one isn't kept, and whole lists leave, least recently used first. */
    @Test
    fun listBudget() {
        val a = live
        fun list(series: String, n: Int) = List(n) { row("$series-$it", series) }
        memo.putList(a, volumes("big"), list("big", 21))
        assertEquals(null, memo.list(a, volumes("big")))
        memo.putList(a, volumes("s1"), list("s1", 15))
        memo.putList(a, volumes("s2"), list("s2", 15))
        memo.list(a, volumes("s1"))
        memo.putList(a, volumes("s3"), list("s3", 10))
        // 40 rows over the budget of 30: s2, the least recently used, goes, then 25 fits.
        assertEquals(listOf(true, false, true), listOf("s1", "s2", "s3").map { memo.list(a, volumes(it)) != null })
    }

    /** The display hint is bound to the generation too, and never feeds the memo: a hint is no row. */
    @Test
    fun hintsAreDisplayOnlyAndGenerationBound() {
        val hints = ShownHints({ live })
        val a = live
        hints.put(a, mapOf("c1" to UserData(starred = true)))
        assertEquals(true, hints.get(a, "c1")?.starred)
        // Nothing of it is a row of the memo, whichever way it is asked.
        assertEquals(null, memo.get(a, "c1"))
        live = "store-b"
        assertEquals(listOf(false, null), listOf(hints.contains(live, "c1"), hints.get(live, "c1")))
        // Work of the old generation neither reads nor writes the new one.
        hints.put(a, mapOf("c2" to null))
        assertEquals(false, hints.contains(live, "c2"))
        // A series' downloads-line presence is remembered per generation as well.
        hints.putFlag(live, "s1", true)
        val b = live
        assertEquals(true, hints.flag(b, "s1"))
        live = "store-c"
        assertEquals(null, hints.flag(live, "s1"))
    }
}
