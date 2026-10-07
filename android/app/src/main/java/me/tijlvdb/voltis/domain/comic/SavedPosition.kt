package me.tijlvdb.voltis.domain.comic

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import me.tijlvdb.voltis.domain.numberOrNull

/**
 * The page a saved position (`{current_page, progress_percent, at_end}`) opens on: the last page
 * when it is at the end, else its page, clamped; 0 without one.
 */
fun pageFor(progress: JsonObject?, pageCount: Int): Int {
    val last = (pageCount - 1).coerceAtLeast(0)
    if ((progress?.get("at_end") as? JsonPrimitive)?.booleanOrNull == true) return last
    val page = progress?.get("current_page").numberOrNull()
    return if (page == null || page.isNaN()) 0 else page.toInt().coerceIn(0, last)
}
