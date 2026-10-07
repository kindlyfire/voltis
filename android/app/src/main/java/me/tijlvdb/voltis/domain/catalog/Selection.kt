package me.tijlvdb.voltis.domain.catalog

import me.tijlvdb.voltis.data.api.Content

/** A selected item as it was fetched: what a reading command is made against. [series] is its series' title, for a notice. */
data class Selected(val content: Content, val series: String? = null)

/** A grid's select mode and its items, in the order they were picked. */
data class Selection(val active: Boolean = false, val items: Map<String, Selected> = emptyMap()) {
    /** Selects or deselects [content]; null when it would be the item past [MAX_SELECTION]. */
    fun toggle(content: Content, series: String? = null): Selection? = when {
        content.id in items -> copy(items = items - content.id)
        items.size >= MAX_SELECTION -> null
        else -> copy(active = true, items = items + (content.id to Selected(content, series)))
    }

    /** After a filter, scope or sort change: nothing selected, still in select mode. */
    fun cleared() = copy(items = emptyMap())

    /** A series' Completed can include its unread volumes. */
    val mayHaveSeries get() = items.values.any { it.content.isSeries }
}

/** A safety net, mentioned only when a tap meets it. */
const val MAX_SELECTION = 500
