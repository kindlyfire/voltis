package me.tijlvdb.voltis.data.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import me.tijlvdb.voltis.domain.numberOrNull
import me.tijlvdb.voltis.domain.reading.ReadingSeqSerializer

// Hand-ported from frontend/src/utils/api/{misc,types}.ts.

/** The parts of the web's `Info` this app uses. */
@Serializable
data class Info(
    // Defaults let an older server decode, so the app can say it's too old.
    @SerialName("api_version") val apiVersion: Int = 0,
    @SerialName("server_id") val serverId: String = "",
    /** The release string. */
    val version: String = "",
    @SerialName("first_user_flow") val firstUserFlow: Boolean,
    @SerialName("password_login_enabled") val passwordLoginEnabled: Boolean,
    @SerialName("oidc_enabled") val oidcEnabled: Boolean,
    @SerialName("oidc_button_label") val oidcButtonLabel: String,
)

/** The parts of the web's `Me` this app uses. */
@Serializable
data class Me(
    val id: String,
    val username: String,
    /** One of [SessionMethod]; a String so a new method doesn't break decoding. */
    @SerialName("session_method") val sessionMethod: String,
    val email: String? = null,
    val permissions: List<String> = emptyList(),
    /** Free JSON, not necessarily an object: read it through [prefs]. */
    val preferences: JsonElement = JsonNull,
) {
    val prefs get() = UserPrefs(preferences)
    val isAdmin get() = "ADMIN" in permissions
}

/** The answer to a preferences patch: a plain user, without the session's fields, so not a [Me]. */
@Serializable
data class User(val preferences: JsonElement = JsonNull)

/** A typed, read-only view over a user's preferences. Anything but an object reads as empty. */
class UserPrefs(preferences: JsonElement) {
    private val root = preferences as? JsonObject
    private val libraries = root?.get("libraries") as? JsonObject

    /** One of [LibraryVisibility], or null when unset. */
    fun libraryVisibility(libraryId: String): String? =
        ((libraries?.get(libraryId) as? JsonObject)?.get("visibility") as? JsonPrimitive)?.contentOrNull

    /** The reading speeds behind the time estimates, with the web's defaults. */
    val wordsPerMinute get() = readingSpeed("wordsPerMinute") ?: 250.0
    val secondsPerPage get() = readingSpeed("secondsPerPage") ?: 20.0

    private fun readingSpeed(name: String): Double? =
        (root?.get("reading") as? JsonObject)?.get(name).numberOrNull()?.takeIf { it > 0 }
}

object LibraryVisibility {
    const val SHOW = "show"
    const val OVERFLOW = "overflow"
    const val HIDE = "hide"
}

@Serializable
data class Session(
    val id: String,
    /** One of [SessionMethod]. */
    val method: String,
    /** Set for app sessions; null for a browser. */
    @SerialName("client_name") val clientName: String? = null,
    @SerialName("created_at") val createdAt: String,
    @SerialName("last_used_at") val lastUsedAt: String? = null,
    val current: Boolean = false,
)

@Serializable
data class Library(val id: String, val name: String)

object SessionMethod {
    const val PASSWORD = "password"
    const val OIDC = "oidc"
    const val PROXY = "proxy"
}

/** The parts of the web's `Content` this app uses. List rows have an empty [meta] and [fileData]. */
@Serializable
data class Content(
    val id: String,
    val title: String,
    /** One of [ContentType]. */
    val type: String,
    @SerialName("parent_id") val parentId: String? = null,
    @SerialName("library_id") val libraryId: String? = null,
    /** With [libraryId], what the server keys user data by. Null from a server that doesn't send it. */
    val uri: String? = null,
    /** False for a row whose file went missing; lists hold valid rows only. */
    val valid: Boolean = true,
    @SerialName("file_mtime") val fileMtime: String? = null,
    @SerialName("file_size") val fileSize: Long? = null,
    @SerialName("cover_version") val coverVersion: String? = null,
    @SerialName("order_parts") val orderParts: List<Double?> = emptyList(),
    val meta: DisplayMetadata = DisplayMetadata(),
    @SerialName("file_data") val fileData: FileData = FileData(),
    @SerialName("children_count") val childrenCount: Int? = null,
    @SerialName("unread_children_count") val unreadChildrenCount: Int? = null,
    @SerialName("completed_children_count") val completedChildrenCount: Int? = null,
    @SerialName("dropped_children_count") val droppedChildrenCount: Int? = null,
    @SerialName("new_children_count") val newChildrenCount: Int? = null,
    /** Null until the user has touched the item. */
    @SerialName("user_data") val userData: UserData? = null,
    /** Only on a single-item fetch. */
    val length: ContentLength? = null,
    /** Only in the continue and recently_updated sorts. */
    @SerialName("continue") val continueInfo: ContinueInfo? = null,
    /** Only on a single-item fetch of a valid top-level entry. */
    @SerialName("facet_keys") val facetKeys: FacetKeys? = null,
)

/** The Discover key of each value of [DisplayMetadata]'s same field, index for index; null where a value has no page. */
@Serializable
data class FacetKeys(
    val staff: List<String?> = emptyList(),
    val genres: List<String?> = emptyList(),
    val tags: List<String?> = emptyList(),
    val publishers: List<String?> = emptyList(),
)

/** A Discover value: [key] is what the server matches, [name] what it shows (a genre's slug). */
@Serializable
data class Facet(val key: String, val name: String, val count: Int)

@Serializable
data class FacetPage(val data: List<Facet>, val total: Int)

/** [roles] are filled for people only. */
@Serializable
data class FacetEntry(val key: String, val name: String, val count: Int, val roles: List<RoleCount> = emptyList())

@Serializable
data class RoleCount(val role: String, val count: Int)

/** A cover in a list's index: the first entries' content in list order. */
@Serializable
data class CoverRef(val id: String, @SerialName("cover_version") val coverVersion: String? = null)

/** A list in `GET /custom-lists?user=me`, newest first. */
@Serializable
data class CustomListSummary(
    val id: String,
    @SerialName("updated_at") val updatedAt: String,
    val name: String,
    val description: String? = null,
    /** One of [ListVisibility]. */
    val visibility: String,
    @SerialName("entry_count") val entryCount: Int? = null,
    val covers: List<CoverRef> = emptyList(),
)

/** A list with its entries in the server's order. [entryCount] counts entries whose content is gone too. */
@Serializable
data class CustomListDetail(
    val id: String,
    @SerialName("updated_at") val updatedAt: String,
    val name: String,
    val description: String? = null,
    val visibility: String,
    @SerialName("entry_count") val entryCount: Int? = null,
    val entries: List<CustomListEntry> = emptyList(),
)

/** An entry refers to its item by [libraryId] and [uri]; [content] is null when that row is gone. */
@Serializable
data class CustomListEntry(
    val id: String,
    @SerialName("library_id") val libraryId: String,
    val uri: String,
    val content: Content? = null,
    val notes: String? = null,
)

@Serializable
data class CountResponse(val count: Int)

object ListVisibility {
    const val PUBLIC = "public"
    const val PRIVATE = "private"
    const val UNLISTED = "unlisted"
}

object ContentType {
    const val COMIC = "comic"
    const val BOOK = "book"
    const val COMIC_SERIES = "comic_series"
    const val BOOK_SERIES = "book_series"
}

object ReadingStatus {
    const val READING = "reading"
    const val COMPLETED = "completed"
    const val ON_HOLD = "on_hold"
    const val DROPPED = "dropped"
    const val PLAN_TO_READ = "plan_to_read"

    /** In the web's order. */
    val ALL = listOf(READING, COMPLETED, ON_HOLD, DROPPED, PLAN_TO_READ)
}

@Serializable
data class DisplayMetadata(
    @SerialName("alt_titles") val altTitles: List<String>? = null,
    val description: String? = null,
    val staff: List<StaffEntry>? = null,
    val publishers: List<String>? = null,
    val language: String? = null,
    /** Partial ISO: YYYY, YYYY-MM or YYYY-MM-DD. */
    @SerialName("publication_date") val publicationDate: String? = null,
    val genres: List<String>? = null,
    val tags: List<String>? = null,
    val status: String? = null,
    val kind: String? = null,
    /** ComicInfo `Manga`: `Yes`, `No`, `YesAndRightToLeft` or `Unknown`. */
    val manga: String? = null,
    val links: List<MetadataLink>? = null,
)

@Serializable
data class StaffEntry(val name: String, val role: String)

@Serializable
data class MetadataLink(val label: String, val url: String)

@Serializable
data class FileData(
    /** `[name]`, or `[name, width, height]` when asked with `page_sizes=1`. */
    val pages: List<JsonArray>? = null,
)

@Serializable
data class UserData(
    val starred: Boolean = false,
    val status: String? = null,
    val rating: Int? = null,
    /** A comic's is `{current_page (0-based), progress_percent, at_end?}`. */
    val progress: JsonObject? = null,
    val revision: String? = null,
    @SerialName("status_updated_at") val statusUpdatedAt: String? = null,
    @SerialName("progress_updated_at") val progressUpdatedAt: String? = null,
    @SerialName("last_read_at") val lastReadAt: String? = null,
    /** The state's order among the content's states (`reading_seq`); 0 for an untouched item, which has no `user_data`. */
    @SerialName("reading_seq") @Serializable(ReadingSeqSerializer::class) val readingSeq: Long = 0,
)

/** Words for books, pages for comics; [remaining] is what the user has left. */
@Serializable
data class ContentLength(val unit: String, val total: Int, val remaining: Int)

@Serializable
data class ContinueInfo(
    @SerialName("is_new") val isNew: Boolean,
    val series: Content? = null,
)

@Serializable
data class ContentPage(
    val data: List<Content>,
    /** Null when asked with `count=false`. */
    val total: Int? = null,
)

/** A "Continue reading" row: the item to read next, and its series unless standalone. */
@Serializable
data class ContinueEntry(
    val item: Content,
    val series: Content? = null,
    @SerialName("is_new") val isNew: Boolean,
)

@Serializable
data class ContentIds(val ids: List<String>)

/** What reading an item or a series continues with. Without a [target], [reason] says why. */
@Serializable
data class ContinueTarget(
    val target: Content? = null,
    @SerialName("series_id") val seriesId: String? = null,
    /** `start`, `resume` or `next`. */
    val action: String? = null,
    /** One of [ContinueReason]. */
    val reason: String? = null,
    @SerialName("earlier_unread_id") val earlierUnreadId: String? = null,
)

object ContinueReason {
    const val COMPLETED = "completed"
    const val CAUGHT_UP = "caught_up"
    const val EARLIER_UNREAD = "earlier_unread"
    const val HELD = "held"
    const val EMPTY = "empty"
}

@Serializable
data class ErrorResponse(val error: String)

@Serializable
data class TokenRequest(
    val username: String,
    val password: String,
    @SerialName("client_name") val clientName: String,
)

@Serializable
data class ExchangeRequest(
    val code: String,
    @SerialName("code_verifier") val codeVerifier: String,
)

@Serializable
data class TokenResponse(val token: String)
