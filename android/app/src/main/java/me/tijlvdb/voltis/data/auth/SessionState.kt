package me.tijlvdb.voltis.data.auth

import okhttp3.HttpUrl

/** A Voltis server, keyed by its `server_id` so a changed URL keeps the same record. */
data class Server(val id: String, val url: HttpUrl)

sealed interface SessionState {
    val server: Server? get() = null

    /** Stored state hasn't been read yet. */
    data object Loading : SessionState

    data object NoServer : SessionState

    data class SignedOut(override val server: Server) : SessionState

    /** The token was rejected or dropped; the user ID is kept so later phases can keep their data. */
    data class NeedsReauth(override val server: Server, val userId: String) : SessionState

    data class SignedIn(override val server: Server, val userId: String, val username: String) : SessionState
}
