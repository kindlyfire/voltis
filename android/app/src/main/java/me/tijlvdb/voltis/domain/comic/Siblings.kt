package me.tijlvdb.voltis.domain.comic

import kotlinx.coroutines.CancellationException
import me.tijlvdb.voltis.data.api.Content

/** The volumes around the one being read. A port of `pages/read/useSiblings.ts`. */
data class Siblings(
    /**
     * [Status.Ready] once the series' list holds the current volume (always, for standalone
     * content); [Status.Error] when the list failed or came without the current volume, which a
     * scan can move or remove.
     */
    val status: Status,
    /** The series' volumes in order; none for standalone content. */
    val items: List<Content> = emptyList(),
    /** The current one's place among them, or -1. */
    val index: Int = -1,
) {
    enum class Status { Loading, Ready, Error }

    val prev: Content? get() = items.getOrNull(index - 1)
    val next: Content? get() = items.getOrNull(index + 1)
}

/**
 * Loads the volumes around [contentId]. Only its own series' list counts: one without the volume
 * is an error, so nothing mistakes it for the end of the series. A retry sets [reread], which
 * reads the content again first and follows it to the series it is in now.
 */
suspend fun loadSiblings(data: ReaderData, contentId: String, parentId: String?, reread: Boolean = false): Siblings {
    var parent = parentId
    if (reread) {
        try {
            parent = data.content(contentId).parentId
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            // The list below reports the failure.
        }
    }
    if (parent == null) return Siblings(Siblings.Status.Ready)
    val items = try {
        data.volumes(parent)
    } catch (e: CancellationException) {
        throw e
    } catch (_: Exception) {
        return Siblings(Siblings.Status.Error)
    }
    val index = items.indexOfFirst { it.id == contentId }
    return if (index >= 0) Siblings(Siblings.Status.Ready, items, index) else Siblings(Siblings.Status.Error)
}
