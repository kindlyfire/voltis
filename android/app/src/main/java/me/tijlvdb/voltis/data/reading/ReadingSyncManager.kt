package me.tijlvdb.voltis.data.reading

import android.util.Log
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.coroutines.CoroutineContext
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.emitAll
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.onStart
import me.tijlvdb.voltis.data.sync.AccountRead
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.auth.AppScope
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.db.AccountScoped
import me.tijlvdb.voltis.data.db.AccountStores
import me.tijlvdb.voltis.data.downloads.DownloadRepository
import me.tijlvdb.voltis.data.sync.RoomSyncNotices
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.reading.AttentionItem
import me.tijlvdb.voltis.domain.reading.CommandOutcome
import me.tijlvdb.voltis.domain.reading.DrainResult
import me.tijlvdb.voltis.domain.reading.LaneView
import me.tijlvdb.voltis.domain.reading.PromptChoice
import me.tijlvdb.voltis.domain.reading.ReaderAdapter
import me.tijlvdb.voltis.domain.reading.ReaderNotice
import me.tijlvdb.voltis.domain.reading.ReadingCommands
import me.tijlvdb.voltis.domain.reading.ReadingSession
import me.tijlvdb.voltis.domain.reading.ReadingSync
import me.tijlvdb.voltis.domain.reading.SeriesAction
import me.tijlvdb.voltis.domain.reading.Shown
import me.tijlvdb.voltis.domain.reading.SyncPrompt
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.domain.reading.itemCount
import me.tijlvdb.voltis.domain.sync.SyncNotices

/**
 * One [ReadingEngine] for the account whose store is open (P2 §5): started when the store opens,
 * stopped before it closes, and woken by [foreground]. A reader belongs to the account signed in
 * when it attached, and is never handed to another account's engine; so does a content page's
 * command ([ReadingCommands]), by the account signed in when it is made.
 */
@OptIn(ExperimentalCoroutinesApi::class)
@Singleton
class ReadingSyncManager internal constructor(
    /** The account whose store is open, as stores open and close. */
    opened: Flow<String?>,
    /** The signed-in account, read synchronously from the session itself, as navigation reads it. */
    private val signedIn: () -> String?,
    /** Emits whenever [signedIn] may have changed. */
    private val sessionChanges: Flow<*>,
    /** The signed-in account whose store couldn't be opened. */
    private val failed: StateFlow<String?>,
    /** An engine over [account]'s store, not started; null when that store has closed meanwhile. */
    private val newEngine: (account: String) -> ReadingEngine?,
    private val loadIdentity: suspend () -> Unit,
    connectivity: Connectivity,
    /** The stored notices of the open account, listed with the held lanes. */
    private val notices: SyncNotices,
    private val scope: CoroutineScope,
    /** Where readers call from, and where a stand-in session runs. */
    private val main: CoroutineContext,
    /** Before an account's engine stops: its background drain is cancelled and no longer scheduled. */
    private val closeWork: (account: String) -> Unit = {},
) : ReadingSync, ReadingCommands, AccountScoped {
    @Inject constructor(
        stores: AccountStores,
        session: SessionStore,
        api: VoltisApi,
        identity: WriterIdentity,
        events: CatalogEvents,
        notices: RoomSyncNotices,
        connectivity: Connectivity,
        work: SyncWork,
        // Lazy: the repository seeds lanes through this manager.
        downloads: dagger.Lazy<DownloadRepository>,
        @AppScope scope: CoroutineScope,
    ) : this(
        stores.current.map { it?.account },
        { session.state.value.account },
        session.state,
        stores.failed,
        newEngine = { account ->
            stores.current.value?.takeIf { it.account == account }?.let { store ->
                ReadingEngine(
                    // Its requests are refused, unsent, once another account is signed in.
                    RoomReadingStore(store.db, notices::announce), RetrofitReadingTransport(api, account), identity, events, connectivity, work.open(store.dir.name), scope,
                    main = Dispatchers.Main.immediate, onError = { Log.e(TAG, "Reading sync", it) },
                    onGone = { id, at -> scope.launch { downloads.get().markGone(account, id, at) } },
                    accountDir = store.dir.name,
                )
            }
        },
        loadIdentity = identity::load,
        connectivity,
        notices,
        scope,
        Dispatchers.Main.immediate,
        closeWork = { work.close(AccountStores.directoryName(it)) },
    ) {
        stores.register(this)
    }

    private class Bound(val account: String, val engine: ReadingEngine)

    private val bound = MutableStateFlow<Bound?>(null)
    private val lock = Mutex()
    private var identityLoaded = false

    init {
        // A store appears only after the previous one's stop() returned (AccountStores.switchTo).
        scope.launch { opened.collect { account -> account?.let { start(it) } } }
    }

    private suspend fun start(account: String) = lock.withLock {
        if (bound.value != null) return@withLock
        loadIdentityOnce()
        val engine = newEngine(account) ?: return@withLock
        engine.start()
        bound.value = Bound(account, engine)
    }

    override suspend fun stop() = lock.withLock {
        bound.value?.let {
            bound.value = null
            // Before the engine stops: a worker waiting on its drain is cancelled, and nothing schedules another.
            closeWork(it.account)
            it.engine.stop()
        }
        Unit
    }

    /** The app came to the foreground: retry a start that failed and drain the outbox. `NetworkConnectivity.foreground` probes the server. */
    fun foreground() {
        scope.launch {
            lock.withLock { loadIdentityOnce() }
            current()?.run {
                retryStart()
                launch {
                    try {
                        drain()
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: Exception) {
                        // Stopped, or a start that failed again: nothing to drain.
                    }
                }
            }
        }
    }

    /** The app went to the background: an open prompt is dismissed, to be asked again later. */
    fun background() {
        bound.value?.engine?.background()
    }

    /** Once per process. A failure leaves sends failing, as a `writer` file that can't be written does, until the next foreground. */
    private suspend fun loadIdentityOnce() {
        if (identityLoaded) return
        try {
            loadIdentity()
            identityLoaded = true
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Log.e(TAG, "Couldn't load the writer identity", e)
        }
    }

    /** [account]'s engine, if it runs. */
    private fun engineFor(account: String?) = bound.value?.takeIf { account != null && it.account == account }?.engine

    /** The signed-in account's engine, if it runs. */
    private fun current() = engineFor(signedIn())

    /** [account]'s engine once it runs; fails when its store can't be opened, or [account] is no longer signed in. */
    private suspend fun engineOf(account: String?): ReadingEngine {
        if (account == null) throw SyncUnavailable.AccountChanged()
        return combine(bound, failed, sessionChanges) { bound, failed, _ ->
            when {
                signedIn() != account -> throw SyncUnavailable.AccountChanged()
                bound?.account == account -> bound.engine
                failed == account -> throw SyncUnavailable.OfflineData()
                else -> null
            }
        }.filterNotNull().first()
    }

    override fun attach(contentId: String, adapter: ReaderAdapter): ReadingSession {
        // One reading of the account picks the engine: a lagging copy could pick none, or the previous one.
        val account = signedIn()
        val engine = engineFor(account)
        return if (engine != null) Attached(engine, engine.attach(contentId, adapter)) else LateSession(account, contentId, adapter)
    }

    override fun review(account: String, contentId: String) = when {
        signedIn() != account -> throw SyncUnavailable.AccountChanged()
        failed.value == account -> throw SyncUnavailable.OfflineData()
        // Its engine starts with its store: none yet is a store that just closed.
        else -> engineFor(account)?.review(contentId) ?: throw SyncUnavailable.AccountChanged()
    }

    override suspend fun seed(account: String, contents: List<Content>) = engineOf(account).seed(contents)

    override suspend fun pageCount(account: String, contentId: String, count: Int) = engineOf(account).pageCount(contentId, count)

    override fun shown(ids: Set<String>, account: String): Flow<Map<String, Shown>> =
        bound.flatMapLatest { it?.takeIf { b -> b.account == account }?.engine?.shown(ids) ?: flowOf(emptyMap()) }

    // The notices are the open store's, which is the bound engine's account's.
    override fun attention(account: String): Flow<List<AttentionItem>> = bound.flatMapLatest { b ->
        val engine = b?.takeIf { it.account == account }?.engine ?: return@flatMapLatest flowOf(emptyList())
        combine(engine.held, notices.observe()) { held, stored -> held + stored.map { AttentionItem.Notice(it.id, it.contentId, it.title, it.kind, it.detail) } }
    }

    override fun attentionCount(account: String): Flow<Int> = attention(account).map { it.itemCount() }.distinctUntilChanged()

    override suspend fun dismiss(account: String, noticeId: Long) {
        // The engine's own store, in its loop: not whichever store is open by the time this runs.
        engineOf(account).dismissNotice(noticeId)
    }

    override val unsent: Flow<Int> = bound.flatMapLatest { it?.engine?.unsent ?: flowOf(0) }

    /** [unsent] with null until an engine is bound: its first count is then the stored one, not the 0 of "nothing open yet". */
    val unsentOrNull: Flow<AccountRead<Int>?> = bound.flatMapLatest { b ->
        val engine = b?.engine ?: return@flatMapLatest flowOf(null)
        engine.unsent.map { AccountRead(b.account, it) }.onStart<AccountRead<Int>?> { emit(null) }
    }

    /** [attention] with null until [account]'s engine is bound and both sources have read their stored rows. */
    fun attentionOrNull(account: String): Flow<List<AttentionItem>?> = bound.flatMapLatest { b ->
        val engine = b?.takeIf { it.account == account }?.engine ?: return@flatMapLatest flowOf(null)
        combine(engine.held, notices.observe()) { held, stored -> held + stored.map { AttentionItem.Notice(it.id, it.contentId, it.title, it.kind, it.detail) } }
            .onStart<List<AttentionItem>?> { emit(null) }
    }

    /** [account]'s engine drains; throws when it isn't signed in, or its store can't be opened. */
    suspend fun drain(account: String) = engineOf(account).drain()

    /**
     * [SyncWorker]'s drain, for the account whose directory is [accountDir]: once its engine runs (at
     * process start the store opens a moment after the worker may start), and only while that account
     * is signed in ([onlyIfUnsent]: and has unsent ops). Null when it isn't, or its engine stops meanwhile.
     */
    suspend fun drainInBackground(accountDir: String, onlyIfUnsent: Boolean = false): DrainResult? {
        fun ours(account: String?) = account != null && AccountStores.directoryName(account) == accountDir
        // Another account is signed in: no need to wait. Null is a session not read yet, or signed out.
        if (signedIn() != null && !ours(signedIn())) return null
        val engine = withTimeoutOrNull(START_WAIT) { bound.first { ours(it?.account) } }
            ?.takeIf { it.account == signedIn() }?.engine ?: return null
        // The periodic safety net: an empty outbox needs no drain, and so no request.
        if (onlyIfUnsent && engine.unsent.first() == 0) return DrainResult.DONE
        // Engine drains run in its one actor: a one-time and a periodic worker at once share the pass.
        // A start that failed is retried first, queued ahead of the drain, as a foreground drain and a reader's load do.
        engine.retryStart()
        return try {
            engine.drain()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // Still the signed-in account's engine (a start that still fails): retry later. Otherwise it
            // stopped for an account change, which also cancels the work.
            if (bound.value?.engine === engine && ours(signedIn())) DrainResult.WAITING else null
        }
    }

    // ReadingCommands: each goes to the engine of the account signed in as it is called, once that engine runs.

    override suspend fun setStatus(content: Content, status: String?): CommandOutcome =
        engineOf(signedIn()).command(content, statusRequest(status))

    override suspend fun clear(content: Content): CommandOutcome =
        engineOf(signedIn()).command(content, CLEAR_REQUEST)

    override suspend fun markSeriesCompleted(series: Content, includeUnread: Boolean): CommandOutcome =
        engineOf(signedIn()).seriesCommand(series, SeriesAction.MARK_SERIES_COMPLETED, includeUnread = includeUnread)

    override suspend fun markThrough(series: Content, untilId: String): CommandOutcome =
        engineOf(signedIn()).seriesCommand(series, SeriesAction.MARK_THROUGH, untilId = untilId)

    /** Commands for [account] only, tagged with bulk [origin]. Each throws [SyncUnavailable.AccountChanged] once [account] isn't signed in. */
    fun commandsFor(account: String, origin: Long? = null): ReadingCommands = object : ReadingCommands {
        override suspend fun setStatus(content: Content, status: String?) =
            engineOf(account).command(content, statusRequest(status), origin)

        override suspend fun clear(content: Content) =
            engineOf(account).command(content, CLEAR_REQUEST, origin)

        override suspend fun markSeriesCompleted(series: Content, includeUnread: Boolean) =
            engineOf(account).seriesCommand(series, SeriesAction.MARK_SERIES_COMPLETED, includeUnread = includeUnread, origin = origin)

        override suspend fun markThrough(series: Content, untilId: String) =
            engineOf(account).seriesCommand(series, SeriesAction.MARK_THROUGH, untilId = untilId, origin = origin)
    }

    private fun statusRequest(status: String?) = JsonObject(mapOf("op" to JsonPrimitive("set_status"), "status" to JsonPrimitive(status)))

    /** A session of a running engine. Its loads retry a failed start first, as the reader's Retry should. */
    private class Attached(private val engine: ReadingEngine, private val session: ReadingSession) : ReadingSession by session {
        override suspend fun load(): JsonObject {
            engine.retryStart()
            return session.load()
        }
    }

    /**
     * A reader attached before its account's engine runs (a process restored into the reader):
     * calls wait for the engine, in order, and the flows follow its session. Called on [main] only.
     * Attaching fails, and drops what waited, when the store can't be opened or the account changes.
     */
    private inner class LateSession(account: String?, contentId: String, adapter: ReaderAdapter) : ReadingSession {
        private val ready = CompletableDeferred<Attached>()
        private var attached: Attached? = null
        private val waiting = ArrayDeque<(ReadingSession) -> Unit>()
        private var detached = false
        override val view = MutableStateFlow(LaneView())
        override val prompt = MutableStateFlow<SyncPrompt?>(null)

        /** Ends without an error when attaching fails: [load] reports it. */
        override val notices: Flow<ReaderNotice> = flow {
            val session = try {
                ready.await()
            } catch (e: CancellationException) {
                currentCoroutineContext().ensureActive()
                return@flow
            } catch (e: Exception) {
                return@flow
            }
            emitAll(session.notices)
        }

        private val job = scope.launch(main) {
            val engine = try {
                // Checked again as it is attached: the account or the engine may have changed since it was found.
                var found = engineOf(account)
                while (engineFor(account) !== found || signedIn() != account) found = engineOf(account)
                found
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                waiting.clear()
                ready.completeExceptionally(e)
                return@launch
            }
            // detach() cancels this job, so a detached reader is never attached.
            val session = Attached(engine, engine.attach(contentId, adapter))
            attached = session
            ready.complete(session)
            while (waiting.isNotEmpty()) waiting.removeFirst()(session)
            launch { session.view.collect { view.value = it } }
            launch { session.prompt.collect { prompt.value = it } }
        }

        private fun later(call: (ReadingSession) -> Unit) {
            if (detached || ready.isCompleted && attached == null) return
            attached?.let(call) ?: waiting.addLast(call)
        }

        override suspend fun load(): JsonObject = ready.await().load()

        override suspend fun pageCount(count: Int) = ready.await().pageCount(count)

        override fun invalidateRestores() = later { it.invalidateRestores() }

        override fun moved(progress: JsonObject) = later { it.moved(progress) }

        override fun placed(progress: JsonObject) = later { it.placed(progress) }

        override fun finish(progress: JsonObject) = later { it.finish(progress) }

        override fun flush() = later { it.flush() }

        override suspend fun command(request: JsonObject) = ready.await().command(request)

        override suspend fun seriesCommand(status: String) = ready.await().seriesCommand(status)

        override fun resetAndReadAgain() = later { it.resetAndReadAgain() }

        override fun completeSeries() = later { it.completeSeries() }

        override fun trackProgress() = later { it.trackProgress() }

        override fun retry() = later { it.retry() }

        override fun check() = later { it.check() }

        override fun setVisible(visible: Boolean) = later { it.setVisible(visible) }

        /** Terminal at once: an unattached reader is never attached later, an attached one detaches once. */
        override fun detach() {
            if (detached) return
            detached = true
            waiting.clear()
            job.cancel()
            val session = attached
            if (session != null) session.detach() else ready.cancel()
        }

        override fun answer(choice: PromptChoice) = later { it.answer(choice) }

        override fun dismissPrompt() = later { it.dismissPrompt() }
    }

    private companion object {
        const val TAG = "ReadingSync"
        val CLEAR_REQUEST = JsonObject(mapOf("op" to JsonPrimitive("clear")))

        /** How long the worker waits for its account's engine at process start. */
        const val START_WAIT = 10_000L

    }
}
