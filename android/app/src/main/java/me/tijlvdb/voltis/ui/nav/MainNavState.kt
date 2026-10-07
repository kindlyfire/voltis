package me.tijlvdb.voltis.ui.nav

import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.SnapshotStateList
import androidx.compose.runtime.toMutableStateList
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException
import me.tijlvdb.voltis.data.api.AppJson

/** Downloads is one target whatever its presentation flag; any other destination by equality. */
private fun Dest.sameTarget(other: Dest) = if (this is DownloadsDest && other is DownloadsDest) seriesId == other.seriesId else this == other

/** The signed-in app's back stacks: one per tab, and the reader above them all. */
@Stable
class MainNavState private constructor(saved: Saved?) {
    constructor() : this(null)

    @Serializable
    private data class Saved(val stacks: Map<Tab, List<Entry>>, val current: Tab, val reader: Entry?, val nextUid: Long)

    private var nextUid = saved?.nextUid ?: 0L

    /** Each starts with its root. */
    val stacks: Map<Tab, SnapshotStateList<Entry>> = Tab.entries.associateWith { tab ->
        (saved?.stacks?.get(tab) ?: listOf(entry(tab.root))).toMutableStateList()
    }

    var current by mutableStateOf(saved?.current ?: Tab.Home)
        private set

    var reader by mutableStateOf(saved?.reader)
        private set

    /**
     * What NavDisplay shows: the other tabs' stacks, then the current tab's, then the reader. The
     * others stay in the list so their state is kept; Home is the last of them, as that's where
     * Back goes from another tab's root.
     */
    val entries: List<Entry>
        get() = Tab.entries.filter { it != current }.sortedBy { it == Tab.Home }.flatMap(stacks::getValue) +
            stacks.getValue(current) + listOfNotNull(reader)

    /** Every tab at its root on Home, the reader closed: a fresh back stack (P2 §10). */
    val atRoots: Boolean get() = reader == null && current == Tab.Home && stacks.values.all { it.size == 1 }

    /** The tab [entry] is in; null for the reader. */
    fun tabOf(entry: Entry): Tab? = Tab.entries.firstOrNull { tab -> stacks.getValue(tab).any { it.uid == entry.uid } }

    /** Reselecting the current tab pops it to its root. */
    fun select(tab: Tab) {
        if (tab == current) stacks.getValue(tab).run { removeRange(1, size) }
        current = tab
    }

    /** Appends to the current tab. */
    fun open(dest: Dest) {
        stacks.getValue(current) += entry(dest)
    }

    /** [dest] on top of the Libraries tab, from outside the app (the download notification, the Downloads shortcut). */
    fun showInLibraries(dest: Dest) {
        reader = null
        current = Tab.Libraries
        if (!stacks.getValue(Tab.Libraries).last().dest.sameTarget(dest)) open(dest)
    }

    /** Replaces an open reader: Back from the next volume leaves the reader instead of walking through volumes. */
    fun openReader(contentId: String) {
        reader = entry(ReaderDest(contentId))
    }

    fun closeReader(thenOpen: Dest? = null) {
        reader = null
        if (thenOpen != null) open(thenOpen)
    }

    /** Leaves the reader, else the current tab's top entry, else the tab for Home. At the Home root Back is the system's. */
    fun back() {
        val stack = stacks.getValue(current)
        when {
            reader != null -> reader = null
            stack.size > 1 -> stack.removeAt(stack.lastIndex)
            else -> current = Tab.Home
        }
    }

    /** Back to Home with new roots, so every old entry leaves the list and its state is dropped. */
    fun clear() {
        reader = null
        current = Tab.Home
        for ((tab, stack) in stacks) {
            stack.clear()
            stack += entry(tab.root)
        }
    }

    private fun entry(dest: Dest) = Entry(nextUid++, dest)

    companion object {
        val Saver = Saver<MainNavState, String>(
            save = { AppJson.encodeToString(Saved(it.stacks.mapValues { (_, s) -> s.toList() }, it.current, it.reader, it.nextUid)) },
            // State saved by a version with other destinations starts over instead of crashing.
            restore = {
                try {
                    MainNavState(AppJson.decodeFromString<Saved>(it))
                } catch (_: SerializationException) {
                    null
                }
            },
        )
    }
}
