package me.tijlvdb.voltis.domain.reading

import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.JsonDecoder
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonPrimitive
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.UserData

// The reading API of backend/routes/reading.go.

val EmptyProgress = JsonObject(emptyMap())

/**
 * `reading_seq`: orders the committed states of one content's reading row on the server. A decimal
 * string on the wire (a number is read too); absent is 0, which is a row that doesn't exist.
 */
object ReadingSeqSerializer : KSerializer<Long> {
    override val descriptor = PrimitiveSerialDescriptor("ReadingSeq", PrimitiveKind.STRING)

    override fun deserialize(decoder: Decoder): Long = (decoder as JsonDecoder).decodeJsonElement().jsonPrimitive.content.toLong()

    override fun serialize(encoder: Encoder, value: Long) = encoder.encodeString(value.toString())
}

/**
 * An item's reading on the server, complete. [revision] is `<writer_id>:<seq>` for a reader's write,
 * `srv:…` for others. [seq] is its `reading_seq`: of two states of one item, the greater is the later.
 */
@Serializable
data class ReadingState(
    val revision: String? = null,
    val status: String? = null,
    @SerialName("status_updated_at") val statusUpdatedAt: String? = null,
    val progress: JsonObject = EmptyProgress,
    @SerialName("progress_updated_at") val progressUpdatedAt: String? = null,
    @SerialName("last_read_at") val lastReadAt: String? = null,
    @SerialName("reading_seq") @Serializable(ReadingSeqSerializer::class) val seq: Long = 0,
)

/** A content's complete [state] as one server response carried it: what the snapshot table takes in. */
data class ReadingSnapshot(val contentId: String, val state: ReadingState)

/** A series' own reading state, with its volumes' counts. */
@Serializable
data class SeriesInfo(
    val id: String,
    val status: String? = null,
    val revision: String? = null,
    @SerialName("caught_up") val caughtUp: Boolean = false,
    @SerialName("children_count") val childrenCount: Int = 0,
    @SerialName("completed_children_count") val completedChildrenCount: Int = 0,
    @SerialName("dropped_children_count") val droppedChildrenCount: Int = 0,
    @SerialName("status_updated_at") val statusUpdatedAt: String? = null,
    val progress: JsonObject = EmptyProgress,
    @SerialName("progress_updated_at") val progressUpdatedAt: String? = null,
    @SerialName("last_read_at") val lastReadAt: String? = null,
    @SerialName("reading_seq") @Serializable(ReadingSeqSerializer::class) val seq: Long = 0,
) {
    val state get() = ReadingState(revision, status, statusUpdatedAt, progress, progressUpdatedAt, lastReadAt, seq)
}

/** One content's [id] and complete state, as a series write's answer lists them. */
@Serializable
data class ReadingItem(
    val id: String,
    val revision: String? = null,
    val status: String? = null,
    @SerialName("status_updated_at") val statusUpdatedAt: String? = null,
    val progress: JsonObject = EmptyProgress,
    @SerialName("progress_updated_at") val progressUpdatedAt: String? = null,
    @SerialName("last_read_at") val lastReadAt: String? = null,
    @SerialName("reading_seq") @Serializable(ReadingSeqSerializer::class) val seq: Long = 0,
) {
    val snapshot get() = ReadingSnapshot(id, ReadingState(revision, status, statusUpdatedAt, progress, progressUpdatedAt, lastReadAt, seq))
}

/** `series-reading`'s answer: [series] and every affected volume's state at the end of the write, also for a request delivered before ([count] 0). */
@Serializable
data class SeriesReceipt(val count: Int = 0, val series: ReadingItem? = null, val items: List<ReadingItem> = emptyList()) {
    val snapshots get() = listOfNotNull(series?.snapshot) + items.map { it.snapshot }
}

/** `GET …/reading`, and a 409's body. [writer] is the reader that wrote [ReadingState.revision], if one did. */
@Serializable
data class Envelope(val state: ReadingState, val series: SeriesInfo? = null, val writer: String? = null)

/** What Undo restores: an item's state before a write changed it. */
@Serializable
data class Snapshot(
    val status: String? = null,
    val progress: JsonObject = EmptyProgress,
    @SerialName("last_read_at") val lastReadAt: String? = null,
)

/** A series' status before a volume started it, with the series' revision after the start. */
@Serializable
data class SeriesPrevious(
    val revision: String? = null,
    val status: String? = null,
    @SerialName("status_updated_at") val statusUpdatedAt: String? = null,
)

/** A write's answer: the envelope as of the write, with what it did. */
@Serializable
data class ReadingResult(
    val state: ReadingState,
    val series: SeriesInfo? = null,
    val writer: String? = null,
    /** One of [Outcome]. */
    val outcome: String,
    val previous: Snapshot? = null,
    @SerialName("series_previous") val seriesPrevious: SeriesPrevious? = null,
    /** A series clear's: every child's state afterwards. */
    val items: List<ReadingItem> = emptyList(),
) {
    val envelope get() = Envelope(state, series, writer)
}

object Outcome {
    const val STARTED = "started"
    const val MOVED_TO_READING = "moved_to_reading"
    const val SAVED = "saved"
    const val COMPLETED = "completed"
    const val STATUS_SET = "status_set"
    const val CLEARED = "cleared"
    const val RESTORED = "restored"
    const val SERIES_STATUS = "series_status"
    const val NONE = "none"
}

/**
 * A series' own revision and its valid volumes, in the order the server returned them for `sort=order&sort_order=asc`.
 * Never re-sorted here. [series] is the series' own `user_data`, when it came with them.
 */
data class SeriesVolumes(val revision: String?, val volumes: List<VolumeRef>, val series: UserData? = null)

/** [userData] carries the volume's revision, status and `reading_seq`; null is an untouched volume (seq 0). */
data class VolumeRef(val id: String, val userData: UserData?)

/** [id]'s reading as a snapshot: the complete state of [userData], or the untouched one when it is null. */
fun snapshotOf(id: String, userData: UserData?) = ReadingSnapshot(id, userData.readingState())

fun UserData?.readingState() =
    this?.let { ReadingState(revision, status, statusUpdatedAt, progress ?: EmptyProgress, progressUpdatedAt, lastReadAt, readingSeq) } ?: ReadingState()

/** Every state a volume list carries: the series' (when there) and each volume's. */
fun SeriesVolumes.snapshots(seriesId: String) = listOfNotNull(series?.let { snapshotOf(seriesId, it) }) + volumes.map { snapshotOf(it.id, it.userData) }

/** Where a reader stands, as the user reads it. Books add a percentage in roadmap Phase 3. */
sealed interface PositionLabel {
    /** 1-based. */
    data class Page(val number: Int) : PositionLabel

    /** Without a page count: the saved percentage, else the end. */
    data class Percent(val value: Int) : PositionLabel

    data object End : PositionLabel
}

/** What the sync tells the user, mostly as snackbars. */
sealed interface SyncNotice {
    /** "Moved to p.54 from another device", with Undo. */
    data class Followed(val label: PositionLabel) : SyncNotice

    data class StatusElsewhere(val status: String?) : SyncNotice

    /** "Moved to Reading", "Marked completed", "Series moved to Reading": reading overrode a deliberate status. Its action is the Undo. */
    data class UndoOffer(val what: Undoable) : SyncNotice

    /** "Couldn't undo: …", with [error] shown by `toUiText()`. Its action, if any, is the Retry. */
    data class Failed(val what: SyncAction, val error: Throwable) : SyncNotice

    /** "Cleared the status and position" ([SyncAction.CLEAR]), "Marked the series completed" ([SyncAction.MARK_SERIES_COMPLETED]). */
    data class Done(val what: SyncAction) : SyncNotice

    /** A command the disk was too full to store ([CommandOutcome.UNSAVED]): kept in memory, so no Retry. */
    data object NotSaved : SyncNotice

    /** Stored in `sync_notice` (as `NoticeKind`'s kinds) when nobody was there to see it. */
    sealed interface Lasting : SyncNotice {
        val title: String
    }

    /** A command dropped because the item changed on another device. [uncertain]: an earlier send's result is unknown, so it may have landed. */
    data class ChangedElsewhere(override val title: String, val command: String, val uncertain: Boolean = false) : Lasting

    data class Refused(override val title: String, val message: String) : Lasting

    data class Gone(override val title: String) : Lasting
}

/** What a reader's [SyncNotice.Done] or [SyncNotice.Failed] is about. */
enum class SyncAction { CLEAR, UNDO, MOVE_SERIES, MARK_SERIES_COMPLETED }

enum class Undoable { MOVED_TO_READING, MARKED_COMPLETED, SERIES_MOVED_TO_READING }


/** [this] as the `user_data` a volume reference carries: revision, status and seq are what guards and [covered] read. */
fun ReadingState.userData() = UserData(
    status = status, progress = progress, revision = revision, statusUpdatedAt = statusUpdatedAt,
    progressUpdatedAt = progressUpdatedAt, lastReadAt = lastReadAt, readingSeq = seq,
)

/** A comic's page count, when the content says. */
fun Content.pageCount() = fileData.pages?.size ?: length?.takeIf { it.unit == "pages" }?.total
