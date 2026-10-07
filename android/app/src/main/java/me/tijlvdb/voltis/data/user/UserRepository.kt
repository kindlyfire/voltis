package me.tijlvdb.voltis.data.user

import java.util.concurrent.atomic.AtomicInteger
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.JsonObject
import me.tijlvdb.voltis.data.api.Library
import me.tijlvdb.voltis.data.api.Me
import me.tijlvdb.voltis.data.api.Session
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.auth.AppScope
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents

@Singleton
class UserRepository @Inject constructor(
    private val api: VoltisApi,
    private val store: SessionStore,
    private val events: CatalogEvents,
    @AppScope scope: CoroutineScope,
) {
    /** The user as fetched in this session, with the account it was fetched for. */
    private val fetched = MutableStateFlow<Pair<String, Me>?>(null)

    /** Counts session changes, so a fetch that one overtook isn't published. */
    private val session = AtomicInteger()

    /** The signed-in user, loaded when a session starts. Null until then, and once the session ends. */
    val me: StateFlow<Me?> = combine(store.state, fetched) { state, fetched ->
        fetched?.takeIf { it.first == state.account }?.second
    }.stateIn(scope, SharingStarted.Eagerly, null)

    /** One request for the user at a time: a fetch that started before a patch can't land after it and undo it. */
    private val requests = Mutex()

    // After every property it uses: the collector may run at once on another thread.
    init {
        scope.launch {
            store.state.map { it.account }.distinctUntilChanged().collectLatest { user ->
                // Also on sign-out: signing in again as the same user mustn't show the old session's.
                session.incrementAndGet()
                fetched.value = null
                // A failure leaves it null; the screens that need it retry.
                if (user != null) attempt { refresh() }
            }
        }
    }

    suspend fun refresh() = requests.withLock {
        val user = store.state.value.account ?: return
        val started = session.get()
        val me = api.me()
        if (session.get() == started) fetched.value = user to me
    }

    /** Merge-patches the preferences. The answer is a plain user: its preferences go into the held one. */
    suspend fun patchPreferences(patch: JsonObject) = requests.withLock {
        val started = session.get()
        val preferences = api.patchPreferences(patch).preferences
        if (session.get() == started) fetched.update { it?.copy(second = it.second.copy(preferences = preferences)) }
        events.emit(CatalogChange.PreferencesChanged)
    }

    suspend fun sessions(): List<Session> = api.sessions()

    suspend fun revokeSession(id: String) = api.revokeSession(id)

    suspend fun libraries(): List<Library> = api.libraries()
}
