package me.tijlvdb.voltis.data.auth

import java.io.IOException
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.sameOrigin
import me.tijlvdb.voltis.data.db.DeleteIntents
import me.tijlvdb.voltis.data.storage.localWrite
import me.tijlvdb.voltis.domain.storage.StorageFullException
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

/** A browser sign-in in progress; persisted because the process may die while the browser is open. */
@Serializable
data class PendingFlow(val verifier: String, val serverId: String, val startedAt: Long)

/**
 * Server records keyed by `server_id`, the current server, and the pending browser flow, in
 * Preferences DataStore. Tokens and the flow are encrypted with [TokenCipher]; the rest is plain.
 * Everything is mirrored in memory so interceptors can read it synchronously.
 */
class SessionStore(
    private val dataStore: DataStore<Preferences>,
    private val cipher: TokenCipher,
    scope: CoroutineScope,
) : DeleteIntents {
    /** [generation]: which sign-in of this server's record the session is, read from the published snapshot together with the rest. */
    data class Active(val server: Server, val token: String?, val userId: String? = null, val generation: Long = 0) {
        /** As [SessionState.account]: set only while signed in. */
        val account get() = if (token != null && userId != null) "${server.id}/$userId" else null
    }

    /** [generation] counts the record's sign-ins; it isn't stored: only a session captured in this process is compared. */
    private data class Record(val url: HttpUrl, val token: String?, val userId: String?, val username: String?, val generation: Long = 0)

    /** [deleteIntent]: an account whose offline data a sign-out asked to delete; stored with the sign-out, so a kill before the delete keeps it. */
    private data class Snapshot(val records: Map<String, Record>, val currentId: String?, val deleteIntent: String? = null)

    @Serializable
    private data class StoredRecord(
        val url: String,
        val token: String? = null,
        val userId: String? = null,
        val username: String? = null,
    )

    private val mutex = Mutex()
    @Volatile
    private var snapshot: Snapshot? = null
    private val _state = MutableStateFlow<SessionState>(SessionState.Loading)
    val state: StateFlow<SessionState> = _state.asStateFlow()

    init {
        scope.launch {
            mutex.withLock {
                try {
                    loaded()
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    // Shows server entry, in memory only: the next change reads the file again, and fails while it can't.
                    _state.value = SessionState.NoServer
                }
            }
        }
    }

    /** The current server and its token, or null before loading or without a server. */
    fun active(): Active? {
        val snap = snapshot ?: return null
        val id = snap.currentId ?: return null
        val record = snap.records[id] ?: return null
        return Active(Server(id, record.url), record.token, record.userId, record.generation)
    }

    suspend fun server(id: String): Server? = mutex.withLock { loaded().records[id]?.let { Server(id, it.url) } }

    /**
     * Makes [serverId] current at [url]. A known server on the same origin keeps its token. On a
     * new origin the token is dropped, because `server_id` is public and so spoofable; the user ID
     * is kept ([SessionState.NeedsReauth]).
     */
    suspend fun connect(serverId: String, url: HttpUrl) = update { snap ->
        val old = snap.records[serverId]
        val record = when {
            old == null -> Record(url, null, null, null)
            old.url.sameOrigin(url) -> old.copy(url = url)
            else -> old.copy(url = url, token = null)
        }
        snap.copy(records = snap.records + (serverId to record), currentId = serverId)
    }

    /** The sign-in count of [serverId]'s record as published, to compare with a captured [Active.generation]. */
    fun generationOf(serverId: String): Long? = snapshot?.records?.get(serverId)?.generation

    /** Stores a new token and its user, and makes the server current. */
    suspend fun signIn(serverId: String, token: String, userId: String, username: String) = update { snap ->
        val old = snap.records[serverId] ?: return@update snap
        // Another user's offline data stays in its own directory (P2 decision 16).
        snap.copy(
            records = snap.records + (serverId to old.copy(token = token, userId = userId, username = username, generation = old.generation + 1)),
            currentId = serverId,
            // Signed in again: the data is wanted.
            deleteIntent = snap.deleteIntent?.takeUnless { it == "$serverId/$userId" },
        )
    }

    /**
     * Forgets the token and user of [sent], the session the sign-out was asked for, and only while the
     * record still holds that sign-in ([Active.generation]) and user, with that token or none (a 401 of that
     * session). A newer sign-in is kept. False when it didn't
     * apply. [deleteIntent] is stored with it.
     */
    suspend fun signOut(sent: Active, deleteIntent: String? = null): Boolean {
        var applied = false
        update { snap ->
            val record = snap.records[sent.server.id]
            if (snap.currentId != sent.server.id || record == null || record.userId != sent.userId ||
                record.generation != sent.generation || (record.token != null && record.token != sent.token)
            ) {
                return@update snap
            }
            applied = true
            snap.copy(
                records = snap.records + (sent.server.id to record.copy(token = null, userId = null, username = null)),
                deleteIntent = deleteIntent ?: snap.deleteIntent,
            )
        }
        return applied
    }

    override fun isPending(account: String) = snapshot?.deleteIntent == account

    override suspend fun pending(): String? = mutex.withLock { loaded().deleteIntent }

    override suspend fun clear(account: String) {
        update { snap -> if (snap.deleteIntent == account) snap.copy(deleteIntent = null) else snap }
    }

    /**
     * Runs [block] while no session change can be published: sign-ins and sign-outs wait. For a short,
     * non-suspending step that must be atomic with the live session; it must not call back into the store.
     */
    suspend fun pinned(block: () -> Unit) = mutex.withLock { block() }

    /**
     * Drops the token of [sent], the session a request was made with, after its 401: only that server's
     * record, and only while it still holds that user and token, so a newer sign-in is kept. Keeps the user.
     */
    suspend fun markNeedsReauth(sent: Active) = update { snap ->
        val record = snap.records[sent.server.id]
        if (record == null || sent.token == null || record.token != sent.token || record.userId != sent.userId) return@update snap
        snap.copy(records = snap.records + (sent.server.id to record.copy(token = null)))
    }

    /** Back to server entry; the record is kept for a later [connect]. */
    suspend fun clearCurrent() = update { it.copy(currentId = null) }

    suspend fun saveFlow(flow: PendingFlow) {
        val encrypted = withContext(Dispatchers.IO) { cipher.encrypt(AppJson.encodeToString(PendingFlow.serializer(), flow)) }
        localWrite { dataStore.edit { it[FLOW] = encrypted } }
    }

    /** Returns and clears the pending flow. */
    suspend fun takeFlow(): PendingFlow? {
        var encrypted: String? = null
        localWrite { dataStore.edit { encrypted = it[FLOW]; it.remove(FLOW) } }
        return encrypted?.let { withContext(Dispatchers.IO) { cipher.decrypt(it) } }?.let {
            runCatching { AppJson.decodeFromString(PendingFlow.serializer(), it) }.getOrNull()
        }
    }

    private suspend fun update(f: (Snapshot) -> Snapshot) = mutex.withLock {
        val old = loaded()
        val new = f(old)
        if (new != old) {
            persist(new)
            publish(new)
        }
    }

    /** Must hold [mutex]. */
    private suspend fun loaded(): Snapshot {
        snapshot?.let { return it }
        // An unreadable file starts over empty rather than crashing at launch.
        // An unreadable file is not an empty one: nothing is published or cached, so no write can start from a made-up baseline.
        // A corrupt file is replaced by DataStore's own handler (AuthModule) and reads as empty.
        val prefs = try {
            dataStore.data.first()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            throw IOException("The session couldn't be read", e)
        }
        val stored = prefs[RECORDS]?.let { runCatching { AppJson.decodeFromString<Map<String, StoredRecord>>(it) }.getOrNull() }
        var undecryptable = false
        val records = withContext(Dispatchers.IO) {
            stored.orEmpty().mapNotNull { (id, r) ->
                val url = r.url.toHttpUrlOrNull() ?: return@mapNotNull null
                val token = r.token?.let { enc -> cipher.decrypt(enc).also { if (it == null) undecryptable = true } }
                id to Record(url, token, r.userId, r.username)
            }.toMap()
        }
        val snap = Snapshot(records, prefs[CURRENT]?.takeIf { it in records }, prefs[DELETE])
        if (undecryptable) {
            try {
                persist(snap)
            } catch (_: IOException) {
                // Retried on the next write.
            } catch (_: StorageFullException) {
                // The same.
            }
        }
        publish(snap)
        return snap
    }

    private suspend fun persist(snap: Snapshot) {
        // The Keystore is disk and binder I/O; callers include the main thread.
        val stored = withContext(Dispatchers.IO) {
            snap.records.mapValues { (_, r) -> StoredRecord(r.url.toString(), r.token?.let(cipher::encrypt), r.userId, r.username) }
        }
        localWrite {
            dataStore.edit {
                it[RECORDS] = AppJson.encodeToString(stored)
                if (snap.currentId != null) it[CURRENT] = snap.currentId else it.remove(CURRENT)
                if (snap.deleteIntent != null) it[DELETE] = snap.deleteIntent else it.remove(DELETE)
            }
        }
    }

    private fun publish(snap: Snapshot) {
        snapshot = snap
        _state.value = stateOf(snap)
    }

    private fun stateOf(snap: Snapshot): SessionState {
        val id = snap.currentId ?: return SessionState.NoServer
        val record = snap.records[id] ?: return SessionState.NoServer
        val server = Server(id, record.url)
        return when {
            record.userId == null -> SessionState.SignedOut(server)
            record.token == null -> SessionState.NeedsReauth(server, record.userId)
            else -> SessionState.SignedIn(server, record.userId, record.username.orEmpty())
        }
    }

    private companion object {
        val RECORDS = stringPreferencesKey("servers")
        val CURRENT = stringPreferencesKey("current_server")
        val FLOW = stringPreferencesKey("pending_flow")
        val DELETE = stringPreferencesKey("delete_intent")
    }
}
