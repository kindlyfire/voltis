package me.tijlvdb.voltis.domain.reading

import kotlin.math.roundToLong
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.domain.comic.pageFor

// A comic's progress, as `ComicDisplay/createComicState.ts` writes and compares it.

/** Reading at the 0-based [page] of [pages]. Only a finish reaches 100 %. */
fun position(page: Int, pages: Int): JsonObject = JsonObject(
    buildMap {
        put("current_page", JsonPrimitive(page))
        if (pages > 0) {
            // Math.round(page / pages * 1000) / 10, in the same order: halves round up, as there.
            val percent = minOf((page.toDouble() / pages * 1000).roundToLong() / 10.0, 99.9)
            put("progress_percent", JsonPrimitive(if (percent % 1.0 == 0.0) percent.toLong() else percent))
        }
    },
)

fun finishProgress(pages: Int): JsonObject = JsonObject(
    mapOf(
        "current_page" to JsonPrimitive((pages - 1).coerceAtLeast(0)),
        "progress_percent" to JsonPrimitive(100),
        "at_end" to JsonPrimitive(true),
    ),
)

/**
 * The comic reader's [ReaderAdapter]: positions compare by the page they open on, which needs only
 * the page count. [restore] gets that page.
 */
class ComicAdapter(
    private val pages: () -> Int,
    private val content: () -> Content? = { null },
    private val restore: (page: Int) -> Unit = {},
) : ReaderAdapter {
    override fun content() = content.invoke()

    override fun restore(progress: JsonObject) = restore(pageFor(progress, pages()))

    override fun describe(progress: JsonObject) = PositionLabel.Page(pageFor(progress, pages()) + 1)

    override fun samePosition(a: JsonObject, b: JsonObject) = pageFor(a, pages()) == pageFor(b, pages())

    override fun samePlace(a: JsonObject, b: JsonObject) = samePosition(a, b)
}
