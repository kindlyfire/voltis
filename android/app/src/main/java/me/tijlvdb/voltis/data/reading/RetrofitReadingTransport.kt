package me.tijlvdb.voltis.data.reading

import java.io.IOException
import kotlinx.coroutines.CancellationException
import kotlinx.serialization.json.JsonObject
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.ForAccount
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.content.ContentListParams
import me.tijlvdb.voltis.data.net.Missing
import me.tijlvdb.voltis.data.net.VoltisAnswer
import me.tijlvdb.voltis.domain.reading.Envelope
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.reading.ReadingTransport
import me.tijlvdb.voltis.domain.reading.SeriesVolumes
import me.tijlvdb.voltis.domain.reading.VolumeRef
import retrofit2.HttpException

/**
 * The reading API over Retrofit, with every failure turned into a [ReadingFailure]. It never probes: a 401 is [ReadingFailure.SignedOut] only with Voltis' body.
 * Requests are made for [account]: one that executes while another account is signed in is refused before it is sent, as unreachable.
 */
class RetrofitReadingTransport(private val api: VoltisApi, account: String? = null) : ReadingTransport {
    private val tag = account?.let(::ForAccount)

    override suspend fun get(contentId: String, quick: Boolean) = call {
        if (quick) api.reading(contentId, QUICK_SECONDS, QUICK_SECONDS, tag) else api.reading(contentId, account = tag)
    }

    override suspend fun post(contentId: String, body: JsonObject) = call { api.postReading(contentId, body, tag) }

    // Parsing the answer is what makes a portal's 2xx unreachable rather than a success.
    override suspend fun seriesReading(seriesId: String, body: JsonObject) = call { api.seriesReading(seriesId, body, tag) }

    override suspend fun volumes(seriesId: String) = call {
        val series = api.contentById(seriesId, tag)
        val list = api.content(ContentListParams.volumes(seriesId).toQuery(), tag).data
        SeriesVolumes(series.userData?.revision, list.map { VolumeRef(it.id, it.userData) }, series.userData ?: UserData())
    }

    // Not shared with the pending transport beyond [httpFailure]: this one turns every other exception, an account change included, into Unreachable (the engine parks the op).
    private suspend fun <T> call(block: suspend () -> T): T = try {
        block()
    } catch (e: CancellationException) {
        throw e
    } catch (e: HttpException) {
        throw httpFailure(e, conflicts = true)
    } catch (e: ReadingFailure) {
        throw e
    } catch (e: Exception) {
        // No answer, one that didn't parse, or Retrofit's null-body error for an empty 2xx: none is Voltis'.
        throw ReadingFailure.Unreachable(e)
    }

    private companion object {
        const val QUICK_SECONDS = 3
    }
}

/**
 * What an error status means (P2 §10), shared by the reading and the pending transports. [conflicts]: a 409 with a
 * reading state is [ReadingFailure.Conflict]; without it, it is refused like any other 4xx.
 */
internal fun httpFailure(e: HttpException, conflicts: Boolean): ReadingFailure {
    val body = e.response()?.errorBody()?.string()
    val answer = VoltisAnswer.of(e.code(), body)
    if (answer !is VoltisAnswer.Genuine) return ReadingFailure.Unreachable(e)
    val message = answer.message.orEmpty()
    return when (answer.code) {
        401 -> ReadingFailure.SignedOut()
        // Only the reading routes' own content-missing answers drop anything; another 404 keeps the ops.
        404 -> if (message == Missing.CONTENT || message == Missing.VOLUME) ReadingFailure.Gone(message) else ReadingFailure.Unreachable(IOException("404 $message"))
        // Without the state to adopt, a 409 is no answer the engine can act on.
        409 -> if (!conflicts) ReadingFailure.Refused(409, message) else runCatching { AppJson.decodeFromString(Envelope.serializer(), body!!) }
            .fold({ ReadingFailure.Conflict(it) }, { ReadingFailure.Unreachable(it) })
        in 400..499 -> ReadingFailure.Refused(answer.code, message)
        else -> ReadingFailure.ServerError(answer.code, message)
    }
}
