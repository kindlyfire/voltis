package me.tijlvdb.voltis.domain.reading

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.serialization.json.JsonObject
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.domain.sync.NoticeDetail

/** The reading sync as the app uses it (P2 §6): one engine for the open account, every reading write through its outbox. */
interface ReadingSync {
    fun attach(contentId: String, adapter: ReaderAdapter): ReadingSession

    /**
     * "Needs attention": the lane's own adapter, `restore` doing nothing. The engine ends it
     * ([ReviewSession.ended]). Throws `AccountChanged` once [account] isn't the open one.
     */
    fun review(account: String, contentId: String): ReviewSession

    /**
     * Lanes of [account] from content rows, by the seeding rule (P2 §5): a lane takes a row's state only
     * when its `reading_seq` is greater. Throws `AccountChanged` once [account] isn't the open one.
     */
    suspend fun seed(account: String, contents: List<Content>)

    /** Throws `AccountChanged` once [account] isn't the open one. */
    suspend fun pageCount(account: String, contentId: String, count: Int)

    /** The projection (`shown_*`) of [account]'s lanes that exist among [ids]; empty while another account's engine runs. */
    fun shown(ids: Set<String>, account: String): Flow<Map<String, Shown>>

    /** [account]'s lanes awaiting review, and its stored notices; empty while another account's engine runs. */
    fun attention(account: String): Flow<List<AttentionItem>>

    /** How many items [attention] is about: held lanes and notices of one item count once, a notice without an item singly. */
    fun attentionCount(account: String): Flow<Int>

    /** Throws `AccountChanged` once [account] isn't the open one. */
    suspend fun dismiss(account: String, noticeId: Long)

    /** Ops in the outbox. */
    val unsent: Flow<Int>
}

/**
 * A review's session (P2 §5, Held lanes). The engine ends it: once its read found nothing to ask
 * ([ReviewOutcome.RESOLVED]) or failed ([ReviewOutcome.FAILED]), or, after
 * [ReviewOutcome.ASKED], once the dialog slot is free again. [ReadingSession.detach] ends it early.
 */
interface ReviewSession : ReadingSession {
    /** Null until its read is answered. */
    val outcome: StateFlow<ReviewOutcome?>

    /** True once it ended; [outcome] is set before, unless it was detached first. */
    val ended: StateFlow<Boolean>
}

enum class ReviewOutcome { ASKED, RESOLVED, FAILED }

/**
 * What a lane shows: its acknowledged state with the unsent ops applied (P2 §5, Projection).
 * [unsent]: the lane has ops of its own in the outbox. [projected]: what is unsent, its own or a
 * series op that covers it, shows something else than the acknowledged state.
 */
data class Shown(
    val status: String?,
    val progress: JsonObject,
    val lastReadAt: String?,
    val unsent: Boolean,
    val needsReview: Boolean,
    val projected: Boolean = false,
)

/** With [shown]'s status, progress and last-read time when it is [Shown.projected]: a lane with nothing to show may be older than the content. */
/** The web's, with [describe] returning data rather than text. */
interface ReaderAdapter {
    fun content(): Content?

    /** Places the reader at a saved position; placement writes nothing. Called on the main thread. */
    fun restore(progress: JsonObject)

    fun describe(progress: JsonObject): PositionLabel

    /** Close enough for reconciling with another device: no jump, no conflict. */
    fun samePosition(a: JsonObject, b: JsonObject): Boolean

    /** Exactly the same place: a report there trails a finish taken there. */
    fun samePlace(a: JsonObject, b: JsonObject): Boolean
}

/** One reader's (or reviewer's) part in the sync, the web's `attach()` object. */
interface ReadingSession {
    val view: StateFlow<LaneView>

    /** The open question, while this session holds the one dialog slot. Answer with [answer] or [dismissPrompt]. */
    val prompt: StateFlow<SyncPrompt?>

    /** Snackbars, some with an action. */
    val notices: Flow<ReaderNotice>

    /** Reads the saved state after everything queued before, reconciles, and returns where the reader opens. */
    suspend fun load(): JsonObject

    /** The lane's page count, in this session's own engine. Throws once that engine has stopped. */
    suspend fun pageCount(count: Int)

    /** A `restore` posted before this call doesn't run. Writes nothing. */
    fun invalidateRestores()

    /** Real reading, written once the reader pauses. */
    fun moved(progress: JsonObject)

    /** A placement: written by nothing, but it ends a run of positions. */
    fun placed(progress: JsonObject)

    fun finish(progress: JsonObject)

    fun flush()

    /** `set_status`, `mark_completed` or `clear`, without writer fields. A failure throws with its message. */
    suspend fun command(request: JsonObject): CommandOutcome

    /** Sets the status of the item's series ("Resume series" is `reading`). Last writer wins. */
    suspend fun seriesCommand(status: String): CommandOutcome

    /** Asks [SyncPrompt.ConfirmClear], then clears; the outcome is a [SyncNotice.Done] or [SyncNotice.Failed] of [SyncAction.CLEAR]. */
    fun resetAndReadAgain()

    /**
     * Asks [SyncPrompt.ConfirmCompleteSeries], then completes the item's series, with its unread
     * volumes on [PromptChoice.CONFIRM_WITH_UNREAD]. The outcome is a [SyncNotice.Done] or
     * [SyncNotice.Failed] of [SyncAction.MARK_SERIES_COMPLETED]. Does nothing for an item without a series.
     */
    fun completeSeries()

    /** Reading is written again after an Undo stopped it ([LaneView.tracking]). */
    fun trackProgress()

    /** Sends the failed reading again; the failures stay counted until it is saved. */
    fun retry()

    /** Reads next, holding unsent reading until it is answered. Also asks again about a dismissed dialog (the banner's Review). */
    fun check()

    /** False is the web's `hide()`; true its focus check, skipped for a lane whose dialog was dismissed. */
    fun setVisible(visible: Boolean)

    fun detach()

    fun answer(choice: PromptChoice)

    /** "Ask again later": never a choice (P2 decision 33). */
    fun dismissPrompt()
}

/** A lane as its reader shows it. [acked] is null until the first `load()` answers. */
data class LaneView(
    val acked: ReadingState? = null,
    /** The shown status: the acknowledged one with the unsent ops applied. */
    val status: String? = null,
    val series: SeriesInfo? = null,
    val failures: Int = 0,
    /** Changed on another device, waiting to be asked about. */
    val stale: Boolean = false,
    val tracking: Boolean = true,
    val unsent: Boolean = false,
    /** Storage is full and reading of this lane is held in memory only ("Progress not saved: storage is full"). */
    val storageFull: Boolean = false,
    /** The server can't be reached, or signed the account out: unsent ops wait on this device ("Saved on this device"). */
    val parked: Boolean = false,
)

sealed interface SyncPrompt {
    /** `ReadingConflictModal`: [here] is the reader's position, [saved] the other device's. */
    data class Conflict(val kind: ConflictKind, val here: PositionLabel, val saved: PositionLabel) : SyncPrompt

    /**
     * `SeriesHeldModal`: reading moved an item of a series that is on hold or dropped ([status]).
     * [PromptChoice.MOVE] moves the series to Reading, [PromptChoice.KEEP] or a dismissal leaves it,
     * and with [item] set, [PromptChoice.UNDO] undoes what reading did to the item.
     */
    data class SeriesHeld(val status: String, val item: Undoable?) : SyncPrompt

    data object ConfirmClear : SyncPrompt

    /** `MarkSeriesCompletedModal`: [unread] volumes are neither completed nor dropped. */
    data class ConfirmCompleteSeries(val unread: Int) : SyncPrompt
}

enum class ConflictKind { MOVED, COMPLETED, RESET }

/**
 * A prompt's buttons. A conflict's: [STAY] and [GO] (moved), [KEEP] and [RESET] (completed), [CONTINUE] and [START] (reset).
 * The held series': [MOVE], [KEEP], [UNDO]. A confirmation's: [CONFIRM], and for completing a
 * series with its unread volumes, [CONFIRM_WITH_UNREAD].
 */
enum class PromptChoice { STAY, GO, KEEP, RESET, CONTINUE, START, MOVE, UNDO, CONFIRM, CONFIRM_WITH_UNREAD }

/** A snackbar: [notice], with [action] for its Undo or Retry. */
class ReaderNotice(val notice: SyncNotice, val action: (() -> Unit)? = null)

sealed interface AttentionItem {
    data class Held(val contentId: String, val title: String) : AttentionItem

    data class Notice(val id: Long, val contentId: String?, val title: String, val kind: String, val detail: NoticeDetail) : AttentionItem

    /** A star or rating that failed in three runs: Retry or Discard (P4 decision 10). */
    data class Unsynced(val contentId: String, val title: String, val lastError: String?) : AttentionItem
}

/** The items a list of [AttentionItem]s is about: one per content ID, and each notice without one. */
fun List<AttentionItem>.itemCount() = map {
    when (it) {
        is AttentionItem.Held -> it.contentId
        is AttentionItem.Notice -> it.contentId ?: it
        is AttentionItem.Unsynced -> it.contentId
    }
}.distinct().size

/** "Plum Signal · Vol. 4": an item named with its series, when that is known. */
fun titled(series: String?, title: String) = if (series.isNullOrEmpty()) title else "$series · $title"

/**
 * `QUEUED`: the op is stored and goes later; also what a caller hears when the engine stops (an account change), since the account's next engine sends it.
 * `UNSAVED`: the op is kept in memory only, because the disk is full; it is stored and sent when space is back, and lost if the app is killed first.
 */
enum class CommandOutcome { APPLIED, QUEUED, UNSAVED }

/** Why a reader couldn't be attached to its account's sync, or a command couldn't be made. */
sealed class SyncUnavailable(message: String) : Exception(message) {
    /** The account's database couldn't be opened ("Couldn't open offline data"). */
    class OfflineData : SyncUnavailable("Couldn't open offline data")

    /** The account the reader was opened for signed out or was replaced. */
    class AccountChanged : SyncUnavailable("Signed out")

    /** A series command made without the server, for a series whose volumes aren't cached. */
    class NeedsConnection : SyncUnavailable("Needs a connection")
}

enum class DrainResult { DONE, WAITING, BUSY }
