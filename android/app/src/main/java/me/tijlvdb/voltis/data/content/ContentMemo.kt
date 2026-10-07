package me.tijlvdb.voltis.data.content

import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.DisplayMetadata
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.api.FileData

/**
 * The rows every list and fetch of [ContentRepository] returned, last used kept, so a page opened from a
 * card has what the card had on its first frame.
 *
 * Everything is bound to [live], the open account's store (its instance is a generation: it changes with a
 * sign-out, a switch and a restart of the store). The memo is emptied the moment [live] differs from the owner
 * it holds, and an operation made for another owner than the live one (a response that was in flight when
 * the account changed) reads and writes nothing. With no live owner nothing is kept.
 *
 * A list row has no detail-only fields (meta, length, facet keys, file data). It never replaces them in a row
 * that has them; a detail response replaces the row whole.
 */
class ContentMemo(
    private val live: () -> Any?,
    private val maxRows: Int = 400,
    /** Rows across all kept lists, and the longest list kept: an unpaged list of a long series is skipped. */
    private val maxListRows: Int = 600,
    private val maxListSize: Int = 500,
    private val maxLists: Int = 8,
) {
    private class Row(val content: Content, val detail: Boolean)

    private var owner: Any? = null
    private val rows = object : LinkedHashMap<String, Row>(64, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, Row>) = size > maxRows
    }
    private val lists = LinkedHashMap<ContentListParams, List<Content>>(8, 0.75f, true)

    /** Empties the memo when the live owner changed, and says whether [asked] is it. */
    private fun valid(asked: Any?): Boolean {
        val now = live()
        if (now !== owner) {
            owner = now
            rows.clear()
            lists.clear()
        }
        return now != null && asked === now
    }

    fun get(asked: Any?, id: String): Content? = synchronized(this) { if (valid(asked)) rows[id]?.content else null }

    /** Whole lists of a series' contents only: a paged list is never the answer to the next one. */
    fun list(asked: Any?, params: ContentListParams): List<Content>? = synchronized(this) { if (valid(asked)) lists[params.key()] else null }

    /** [detail]: from a single fetch, which is the whole row; else from a list. */
    fun put(asked: Any?, items: List<Content?>, detail: Boolean) = synchronized(this) {
        if (!valid(asked)) return@synchronized
        for (item in items) {
            if (item == null) continue
            val old = rows[item.id]
            rows[item.id] = when {
                detail -> Row(item, true)
                old != null && old.detail -> Row(item.withDetailOf(old.content), true)
                else -> Row(item, false)
            }
        }
    }

    fun putList(asked: Any?, params: ContentListParams, items: List<Content>) = synchronized(this) {
        if (!valid(asked)) return@synchronized
        val parent = params.parentId
        if (parent == null || parent == ContentListParams.TOP_LEVEL || params.limit != null || params.offset != null) return@synchronized
        val key = params.key()
        lists.remove(key)
        if (items.size > maxListSize) return@synchronized
        lists[key] = items
        // Whole lists leave, the least recently used first, until what they hold is within the budget.
        while (lists.size > maxLists || lists.values.sumOf { it.size } > maxListRows) lists.remove(lists.keys.first())
    }

    /** [id] is gone or off limits: it and every list that holds it or lists its children leave. */
    fun forget(asked: Any?, id: String) = synchronized(this) {
        if (!valid(asked)) return@synchronized
        rows.remove(id)
        lists.entries.removeAll { (params, items) -> params.parentId == id || items.any { it.id == id } }
    }

    val size get() = synchronized(this) { rows.size to lists.values.sumOf { it.size } }

    private fun ContentListParams.key() = copy(count = null)
}

/** [this], a list row, with the fields only a single fetch has, from [detail]. */
private fun Content.withDetailOf(detail: Content) = copy(
    meta = if (meta == DisplayMetadata()) detail.meta else meta,
    fileData = if (fileData == FileData()) detail.fileData else fileData,
    length = length ?: detail.length,
    facetKeys = facetKeys ?: detail.facetKeys,
)

/**
 * What a page last showed of an item's reading (status, progress, star, rating): display only, for the first frame of
 * the next page, until its own effective-reading and pending flows have answered. It is never a seed, a command base
 * or a snapshot, and nothing reads it but the display. Bound to the account generation like [ContentMemo].
 */
class ShownHints(private val live: () -> Any?, private val max: Int = 800) {
    private var owner: Any? = null
    private val map = object : LinkedHashMap<String, UserData?>(64, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, UserData?>) = size > max
    }

    private fun valid(asked: Any?): Boolean {
        val now = live()
        if (now !== owner) {
            owner = now
            map.clear()
            flags.clear()
        }
        return now != null && asked === now
    }

    /** Null: nothing was shown. A hint of null user data (nothing set) is [NONE]. */
    fun get(asked: Any?, id: String): UserData? = synchronized(this) { if (valid(asked)) map[id] else null }

    fun contains(asked: Any?, id: String): Boolean = synchronized(this) { valid(asked) && map.containsKey(id) }

    fun put(asked: Any?, shown: Map<String, UserData?>) = synchronized(this) {
        if (valid(asked)) map.putAll(shown)
    }

    fun forget(asked: Any?, id: String) = synchronized(this) {
        if (valid(asked)) {
            map.remove(id)
            flags.remove(id)
        }
    }

    private val flags = HashMap<String, Boolean>()

    /** A yes or no a page showed about [key] (whether a series' downloads line had rows). */
    fun flag(asked: Any?, key: String): Boolean? = synchronized(this) { if (valid(asked)) flags[key] else null }

    fun putFlag(asked: Any?, key: String, value: Boolean) = synchronized(this) {
        if (valid(asked)) {
            if (flags.size > max) flags.clear()
            flags[key] = value
        }
    }
}
