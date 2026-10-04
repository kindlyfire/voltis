package me.tijlvdb.voltis.data.auth

import java.io.IOException
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.emptyPreferences
import androidx.datastore.preferences.core.stringPreferencesKey
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.Serializable
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.sameOrigin
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
) {
    data class Active(val server: Server, val token: String?)

    private data class Record(val url: HttpUrl, val token: String?, val userId: String?, val username: String?)

    private data class Snapshot(val records: Map<String, Record>, val currentId: String?)

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
        scope.launch { mutex.withLock { loaded() } }
    }

    /** The current server and its token, or null before loading or without a server. */
    fun active(): Active? {
        val snap = snapshot ?: return null
        val id = snap.currentId ?: return null
        val record = snap.records[id] ?: return null
        return Active(Server(id, record.url), record.token)
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
        Snapshot(snap.records + (serverId to record), serverId)
    }

    /** Stores a new token and its user, and makes the server current. */
    suspend fun signIn(serverId: String, token: String, userId: String, username: String) = update { snap ->
        val old = snap.records[serverId] ?: return@update snap
        // Later phases: when old.userId differs, wipe that user's offline data here.
        Snapshot(snap.records + (serverId to old.copy(token = token, userId = userId, username = username)), serverId)
    }

    /** Forgets the current server's token and user. */
    suspend fun signOut() = update { snap ->
        val id = snap.currentId ?: return@update snap
        val record = snap.records[id] ?: return@update snap
        snap.copy(records = snap.records + (id to record.copy(token = null, userId = null, username = null)))
    }

    /** Drops [sentToken] after a 401, unless a newer sign-in has replaced it. Keeps the user. */
    suspend fun markNeedsReauth(sentToken: String) = update { snap ->
        snap.copy(records = snap.records.mapValues { (_, r) -> if (r.token == sentToken) r.copy(token = null) else r })
    }

    /** Back to server entry; the record is kept for a later [connect]. */
    suspend fun clearCurrent() = update { it.copy(currentId = null) }

    suspend fun saveFlow(flow: PendingFlow) {
        dataStore.edit { it[FLOW] = cipher.encrypt(AppJson.encodeToString(PendingFlow.serializer(), flow)) }
    }

    /** Returns and clears the pending flow. */
    suspend fun takeFlow(): PendingFlow? {
        var encrypted: String? = null
        dataStore.edit { encrypted = it[FLOW]; it.remove(FLOW) }
        return encrypted?.let(cipher::decrypt)?.let {
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
        val prefs = dataStore.data.catch { if (it is IOException) emit(emptyPreferences()) else throw it }.first()
        val stored = prefs[RECORDS]?.let { runCatching { AppJson.decodeFromString<Map<String, StoredRecord>>(it) }.getOrNull() }
        var undecryptable = false
        val records = stored.orEmpty().mapNotNull { (id, r) ->
            val url = r.url.toHttpUrlOrNull() ?: return@mapNotNull null
            val token = r.token?.let { enc -> cipher.decrypt(enc).also { if (it == null) undecryptable = true } }
            id to Record(url, token, r.userId, r.username)
        }.toMap()
        val snap = Snapshot(records, prefs[CURRENT]?.takeIf { it in records })
        if (undecryptable) {
            try {
                persist(snap)
            } catch (_: IOException) {
                // Retried on the next write.
            }
        }
        publish(snap)
        return snap
    }

    private suspend fun persist(snap: Snapshot) {
        val stored = snap.records.mapValues { (_, r) ->
            StoredRecord(r.url.toString(), r.token?.let(cipher::encrypt), r.userId, r.username)
        }
        dataStore.edit {
            it[RECORDS] = AppJson.encodeToString(stored)
            if (snap.currentId != null) it[CURRENT] = snap.currentId else it.remove(CURRENT)
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
    }
}
