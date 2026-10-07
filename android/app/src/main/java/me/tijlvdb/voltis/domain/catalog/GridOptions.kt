package me.tijlvdb.voltis.domain.catalog

import kotlin.math.max
import kotlin.math.roundToInt
import kotlinx.serialization.Serializable

enum class ItemCountMode { Unread, Total }

/** A grid's display options, as `GridSettings` in `components/ContentGrid/store.ts`. */
@Serializable
data class GridOptions(
    /** In dp. The column count follows from it and the grid's width: see [gridColumns]. The default gives 3 columns on phones. */
    val itemSize: Int = DEFAULT_ITEM_SIZE,
    val hideItemCount: Boolean = false,
    val itemCountMode: ItemCountMode = ItemCountMode.Unread,
    val hideStatus: Boolean = false,
    val hideTitle: Boolean = false,
    val hideProgress: Boolean = false,
    val hideReadingHighlight: Boolean = false,
) {
    /** "Show all": every visibility option off, and the unread count. */
    fun showAll() = GridOptions(itemSize = itemSize)
}

private const val DEFAULT_ITEM_SIZE = 135
private const val MIN_ITEM_SIZE = 120
const val MAX_ITEM_SIZE = 400

/** `roundToInt` rounds a half up, as the web's `Math.round`: 425 dp at 170 is 3 columns. */
fun gridColumns(width: Float, itemSize: Int): Int = max(1, (width / itemSize).roundToInt())

/** The column counts the stepper offers at this width. */
fun columnRange(width: Float): IntRange = gridColumns(width, MAX_ITEM_SIZE)..gridColumns(width, MIN_ITEM_SIZE)

/** The item size that gives [columns] at this width. */
fun itemSizeFor(width: Float, columns: Int): Int = (width / columns).roundToInt()
