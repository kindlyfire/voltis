package me.tijlvdb.voltis.data.reading

import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.db.AccountStore
import me.tijlvdb.voltis.data.db.AccountStores
import me.tijlvdb.voltis.data.db.importReading
import me.tijlvdb.voltis.domain.reading.snapshotOf

/**
 * The account-bound way in for the reading states of content responses outside the reading engine
 * (list, detail, continue): [open] before the request, [accept] with its answer. The states are
 * imported by the primitive, and only into the store the request was made for. The caller tags the
 * request with that store's account (`ForAccount`), which `ServerUrlInterceptor` checks against the live
 * session as the request leaves: an answer is only ever another account's if it was never sent.
 */
@Singleton
class ReadingIngest(private val current: () -> AccountStore?) {
    @Inject
    constructor(stores: AccountStores) : this({ stores.current.value })

    /** The store open now, or null; with [account], only that account's store: a request for another account imports nothing. */
    fun open(account: String? = null): AccountStore? = current()?.takeIf { account == null || it.account == account }

    suspend fun accept(asked: AccountStore?, contents: Collection<Content?>) {
        if (asked == null || current() !== asked) return
        try {
            asked.db.importReading(contents.filterNotNull().map { snapshotOf(it.id, it.userData) })
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // The answer is still the caller's; the next fetch brings the states again.
        }
    }

    companion object {
        val None = ReadingIngest { null }
    }
}
