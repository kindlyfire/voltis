package me.tijlvdb.voltis.domain.reading

import kotlinx.serialization.json.JsonObject

/** The reading API as the engine uses it. Every call throws one of [ReadingFailure] when it doesn't succeed. */
interface ReadingTransport {
    /** [quick]: 3 s connect and read timeouts. */
    suspend fun get(contentId: String, quick: Boolean): Envelope

    /** [body] is sent as is: its nulls (`status`, `base_revision`, a snapshot's) must reach the server. */
    suspend fun post(contentId: String, body: JsonObject): ReadingResult

    /** `series-reading`: `mark_through`, `mark_series_completed`, `clear`. Answers with the states it left. */
    suspend fun seriesReading(seriesId: String, body: JsonObject): SeriesReceipt

    /** The series' row and its volumes (`sort=order&sort_order=asc`), used as returned. */
    suspend fun volumes(seriesId: String): SeriesVolumes
}

/** How a request didn't succeed; only a genuine Voltis answer is anything but [Unreachable] (P2 §10). */
sealed class ReadingFailure(message: String?, cause: Throwable? = null) : Exception(message, cause) {
    /** A 409: another write got there first. [current] is the server's state now. */
    class Conflict(val current: Envelope) : ReadingFailure("Changed on another device")

    /** A Voltis 4xx other than 401, 404 and 409: it can never succeed. */
    class Refused(val code: Int, message: String) : ReadingFailure(message)

    /** A 404 with Voltis' body. */
    class Gone(message: String) : ReadingFailure(message)

    /** A 401 with Voltis' body. */
    class SignedOut : ReadingFailure("Signed out")

    /** A Voltis 5xx: it may work later. */
    class ServerError(val code: Int, message: String?) : ReadingFailure(message)

    /** No genuine answer: a network error, a timeout, a gateway, a portal. */
    class Unreachable(cause: Throwable? = null) : ReadingFailure("Can't reach the server", cause)

    /**
     * Not an answer: the engine dropped [command] (one of [SyncCommand]) because what it was made
     * against changed on another device (P2 §5, Guards). This attempt wasn't sent; an earlier send's
     * result may be unknown.
     */
    class ChangedElsewhere(val command: String, val uncertain: Boolean = false) : ReadingFailure("Changed on another device")
}
