package me.tijlvdb.voltis.data.api

import java.io.IOException
import java.net.ConnectException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import javax.net.ssl.SSLException
import kotlinx.serialization.SerializationException
import kotlinx.coroutines.CancellationException
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.auth.AuthException
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
    is LocalNetworkException -> UiText.Res(R.string.error_local_network)
    is HttpException -> response()?.serverMessage()?.let(UiText::Raw) ?: UiText.Res(R.string.error_http, code())
    is SerializationException -> UiText.Res(R.string.error_not_json)
    is UnknownHostException -> UiText.Res(R.string.error_unknown_host)
    is SSLException -> UiText.Res(R.string.error_tls)
    is ConnectException, is SocketTimeoutException -> UiText.Res(R.string.error_unreachable)
    is IOException -> UiText.Res(R.string.error_network)
    else -> UiText.Res(R.string.error_unexpected)
}

/** Runs [block] and returns its failure as text, or null; cancellation propagates. */
suspend fun attempt(block: suspend () -> Unit): UiText? = try {
    block()
    null
} catch (e: CancellationException) {
    throw e
} catch (e: Exception) {
    e.toUiText()
}
