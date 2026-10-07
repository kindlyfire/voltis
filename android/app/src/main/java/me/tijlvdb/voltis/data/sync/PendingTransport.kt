package me.tijlvdb.voltis.data.sync

import kotlinx.coroutines.CancellationException
import kotlinx.serialization.json.JsonObject
import me.tijlvdb.voltis.data.api.AccountChangedException
import me.tijlvdb.voltis.data.api.ForAccount
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.reading.httpFailure
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import retrofit2.HttpException

/** The requests of the pending rows. */
interface PendingTransport {
    /**
     * `POST /content/{id}/user-data` with [body] as it is (a cleared rating is a null). Throws one of
     * [ReadingFailure], or [AccountChangedException] when another account is signed in: that one is no answer, and no probe.
     */
    suspend fun updateUserData(contentId: String, body: JsonObject): UserData
}

/** Requests tagged with [account]: one that executes after another account signed in is refused before it leaves. */
class RetrofitPendingTransport(private val api: VoltisApi, account: String) : PendingTransport {
    private val tag = ForAccount(account)

    override suspend fun updateUserData(contentId: String, body: JsonObject): UserData = try {
        api.updateUserData(contentId, body, tag)
    } catch (e: CancellationException) {
        throw e
    } catch (e: AccountChangedException) {
        throw e
    } catch (e: HttpException) {
        throw httpFailure(e, conflicts = false)
    } catch (e: Exception) {
        // No answer, or a 2xx that didn't parse: none is Voltis'.
        throw ReadingFailure.Unreachable(e)
    }
}
