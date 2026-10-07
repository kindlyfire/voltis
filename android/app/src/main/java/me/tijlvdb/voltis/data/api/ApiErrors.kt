package me.tijlvdb.voltis.data.api

import java.io.IOException
import java.net.ConnectException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import javax.net.ssl.SSLException
import kotlinx.serialization.SerializationException
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.auth.AuthException
import me.tijlvdb.voltis.data.downloads.QueueSubmitFailed
import me.tijlvdb.voltis.domain.downloads.PageDamaged
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.domain.storage.StorageFullException
import me.tijlvdb.voltis.ui.UiText
import retrofit2.HttpException
import retrofit2.Response

/** The server's `{"error": …}` message, if the body has one. */
fun Response<*>.serverMessage(): String? = runCatching {
    val body = errorBody()?.string() ?: return null
    AppJson.decodeFromString(ErrorResponse.serializer(), body).error
}.getOrNull()

fun Throwable.toUiText(): UiText = when (this) {
    is AuthException -> text
    is StorageFullException -> UiText.Res(R.string.error_storage_full)
    is QueueSubmitFailed -> UiText.Res(R.string.error_downloads_not_started)
    is PageDamaged -> UiText.Res(R.string.error_page_damaged)
    is LocalNetworkException -> UiText.Res(R.string.error_local_network)
    is ReadingFailure.Unreachable -> if (cause is UnexpectedResponse) UiText.Res(R.string.error_unexpected_response) else UiText.Res(R.string.error_unreachable)
    is SyncUnavailable.OfflineData -> UiText.Res(R.string.error_offline_data)
    is SyncUnavailable.AccountChanged -> UiText.Res(R.string.error_signed_out)
    is SyncUnavailable.NeedsConnection -> UiText.Res(R.string.error_needs_connection)
    is ReadingFailure.Conflict, is ReadingFailure.ChangedElsewhere -> UiText.Res(R.string.sync_changed_elsewhere)
    is ReadingFailure.SignedOut -> UiText.Res(R.string.error_signed_out)
    is ReadingFailure -> message?.let(UiText::Raw) ?: UiText.Res(R.string.error_unexpected)
    is HttpException -> response()?.serverMessage()?.let(UiText::Raw) ?: UiText.Res(R.string.error_http, code())
    is UnexpectedResponse -> UiText.Res(R.string.error_unexpected_response)
    is SerializationException -> UiText.Res(R.string.error_not_json)
    is UnknownHostException -> UiText.Res(R.string.error_unknown_host)
    is SSLException -> UiText.Res(R.string.error_tls)
    is ConnectException, is SocketTimeoutException -> UiText.Res(R.string.error_unreachable)
    is IOException -> UiText.Res(R.string.error_network)
    else -> UiText.Res(R.string.error_unexpected)
}

/**
 * [block]'s outcome. A cancellation propagates, and in a cancelled job a failure is rethrown as the
 * cancellation: a superseded request's failure never becomes an error on screen.
 */
suspend fun <T> attemptResult(block: suspend () -> T): Result<T> = try {
    Result.success(block())
} catch (e: CancellationException) {
    throw e
} catch (e: Exception) {
    currentCoroutineContext().ensureActive()
    Result.failure(e)
}

/** Runs [block] and returns its failure as text, or null. */
suspend fun attempt(block: suspend () -> Unit): UiText? = attemptResult(block).exceptionOrNull()?.toUiText()
