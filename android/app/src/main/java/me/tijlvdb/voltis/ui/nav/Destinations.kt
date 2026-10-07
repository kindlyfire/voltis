package me.tijlvdb.voltis.ui.nav

import kotlinx.serialization.Serializable

@Serializable
sealed interface Dest

// Tab roots.
@Serializable data object HomeDest : Dest
@Serializable data object LibrariesDest : Dest
@Serializable data object SearchDest : Dest
@Serializable data object SettingsDest : Dest

/** [hero] is the origin of the card that opened it, which its cover grows out of (see [HeroKey]). */
@Serializable data class ContentDest(val id: String, val hero: String? = null) : Dest
@Serializable data class LibraryDest(val id: String) : Dest

/** All libraries. A sort seeds the grid's filters (Home's "see all" arrows). */
@Serializable data class BrowseDest(val sort: String? = null, val sortOrder: String? = null) : Dest

@Serializable data object DiscoverDest : Dest

/** [seriesId] opens with that series' volumes shown. [landing]: the screen an offline start opens on, with no Back arrow. */
@Serializable data class DownloadsDest(val seriesId: String? = null, val landing: Boolean = false) : Dest

/** "Needs attention", opened from Downloads. */
@Serializable data object AttentionDest : Dest

@Serializable data object ListsDest : Dest

@Serializable data class ListDest(val id: String) : Dest

/** A Discover value's grid; [kind] is one of `FacetKind`, [key] the server's key. [libraryId] seeds its library. */
@Serializable data class FacetDest(val kind: String, val key: String, val libraryId: String? = null) : Dest

@Serializable data object SessionsDest : Dest
@Serializable data object ReaderDefaultsDest : Dest
@Serializable data object LibraryVisibilityDest : Dest
@Serializable data object DownloadSettingsDest : Dest

/** Always opens at the saved position. */
@Serializable data class ReaderDest(val contentId: String) : Dest

enum class Tab(val root: Dest) {
    Home(HomeDest),
    Libraries(LibrariesDest),
    Search(SearchDest),
    Settings(SettingsDest),
}

/** [uid] keeps two entries for the same destination (in two tabs, say) distinct. */
@Serializable
data class Entry(val uid: Long, val dest: Dest)
