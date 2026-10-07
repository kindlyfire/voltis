package me.tijlvdb.voltis.ui.nav

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.channelFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.reading.BulkRunner
import me.tijlvdb.voltis.data.reading.BulkSummary
import me.tijlvdb.voltis.data.storage.StorageAlerts
import me.tijlvdb.voltis.data.sync.SyncCenter
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.net.settled
import me.tijlvdb.voltis.domain.sync.StoredNotice
import me.tijlvdb.voltis.domain.sync.SyncNotices

/** What the signed-in app's frame shows from the sync: the attention badge, the notices as they are stored, and whether the server is there. */
@OptIn(ExperimentalCoroutinesApi::class)
@HiltViewModel
class ScaffoldViewModel @Inject constructor(
    sync: SyncCenter,
    notices: SyncNotices,
    private val session: SessionStore,
    private val bulk: BulkRunner,
    private val connectivity: Connectivity,
) : ViewModel() {
    val online = connectivity.online

    /** A local write failed for lack of space (at most one per half minute). */
    val storageFull: Flow<Unit> = StorageAlerts.full

    suspend fun probe() = connectivity.probe()

    fun serverUrl() = session.active()?.server?.url

    /** After the start's check (at most a few seconds): the server can't be reached. */
    suspend fun offlineAtStart(): Boolean {
        connectivity.settled()
        return !connectivity.online.value
    }

    val attention = session.state.map { it.account }.distinctUntilChanged().flatMapLatest { it?.let(sync::attentionCount) ?: flowOf(0) }

    /** The head of the signed-in account's queue. */
    val summary: StateFlow<BulkSummary?> =
        combine(bulk.summaries, session.state.map { it.account }) { q, account -> q.firstOrNull { it.account == account } }
            .stateIn(viewModelScope, SharingStarted.Eagerly, null)

    /** Synchronous: at host admission, after the result, before View. */
    fun pending(s: BulkSummary) = bulk.pending(s) && session.state.value.account == s.account

    /** Turns false the moment the account changes, the store closes or it is acknowledged. */
    fun valid(s: BulkSummary): Flow<Boolean> =
        combine(bulk.pendingFlow(s), session.state) { p, st -> p && st.account == s.account }.distinctUntilChanged()

    fun presented(s: BulkSummary) = bulk.acknowledge(s.id)

    /** Notices stored close together (a reconnect's drain), as one batch: one snackbar. */
    val fresh: Flow<List<StoredNotice>> = channelFlow {
        val batch = mutableListOf<StoredNotice>()
        var armed = false
        // Counted from the first notice, not restarted by each: a long drain still flushes.
        notices.fresh.collect { notice ->
            batch += notice
            if (!armed) {
                armed = true
                launch {
                    delay(BATCH_WAIT)
                    val out = batch.toList()
                    batch.clear()
                    armed = false
                    send(out)
                }
            }
        }
    }

    private companion object {
        const val BATCH_WAIT = 1500L
    }
}
