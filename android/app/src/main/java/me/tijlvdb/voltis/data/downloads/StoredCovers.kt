package me.tijlvdb.voltis.data.downloads

import java.io.File
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import me.tijlvdb.voltis.data.api.PLACEHOLDER_HOST
import me.tijlvdb.voltis.data.auth.AppScope
import me.tijlvdb.voltis.domain.net.Connectivity
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

/**
 * The covers stored in the open account's copies (P2 §8), by content ID, while offline; empty
 * online, where the server's (possibly newer) covers are used. An item's is its copy's
 * `cover.jpg`; a series' the `series.jpg` of its most recently completed copy that has one. Each
 * path belongs to one immutable copy, so it is read from the rows alone.
 */
@OptIn(ExperimentalCoroutinesApi::class)
@Singleton
class StoredCovers @Inject constructor(downloads: DownloadRepository, connectivity: Connectivity, @AppScope scope: CoroutineScope) {
    private val files = downloads.current.flatMapLatest { store ->
        store ?: return@flatMapLatest flowOf(emptyMap())
        val root = File(store.accountStore.dir, "downloads")
        store.accountStore.db.downloads().marks().map { rows ->
            val withCopy = rows.filter { it.copyId != null }
            val series = withCopy.filter { it.copySeriesCover && it.seriesId != it.contentId }
                .groupBy { it.seriesId }
                .mapValues { (_, copies) -> File(root, "${copies.maxBy { it.completedAt ?: 0 }.copyId}/$SERIES_COVER") }
            series + withCopy.filter { it.copyCover }.associate { it.contentId to File(root, "${it.copyId}/$COVER") }
        }.flowOn(Dispatchers.Default).closedAs(emptyMap())
    }

    val offline: StateFlow<Map<String, File>> = combine(connectivity.online, files) { online, covers -> if (online) emptyMap() else covers }
        .stateIn(scope, SharingStarted.Eagerly, emptyMap())
}

/** The stored cover for a cover URL (`api/files/cover/<id>`), if [this] has one. */
fun Map<String, File>.forUrl(url: String?): File? {
    if (isEmpty() || url == null) return null
    val path = url.toHttpUrlOrNull()?.takeIf { it.host == PLACEHOLDER_HOST }?.pathSegments ?: return null
    if (path.size != 4 || path[0] != "api" || path[1] != "files" || path[2] != "cover") return null
    return this[path[3]]
}
