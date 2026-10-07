package me.tijlvdb.voltis.data.auth

import android.os.Build
import android.util.Log
import coil3.ImageLoader
import dagger.Lazy
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.withContext
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.ExchangeRequest
import me.tijlvdb.voltis.data.api.ServerUrl
import me.tijlvdb.voltis.data.api.TokenRequest
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.api.api
import me.tijlvdb.voltis.data.api.serverMessage
import me.tijlvdb.voltis.data.db.AccountStores
import me.tijlvdb.voltis.domain.net.MIN_API_VERSION
import me.tijlvdb.voltis.ui.UiText
import okhttp3.HttpUrl
import retrofit2.HttpException

/** A failure with a message for the user. */
class AuthException(val text: UiText) : Exception()

@Singleton
class AuthRepository @Inject constructor(
    private val api: VoltisApi,
    private val store: SessionStore,
    private val images: Lazy<ImageLoader>,
    private val stores: AccountStores,
) {
    /**
     * Checks [url] is a Voltis server this app supports and makes it current. A redirect (http to
     * https, say) is followed, and the address it ends at is the one stored.
     */
    suspend fun connect(url: HttpUrl) {
        val response = api.probeInfo(url.api(ServerUrl.INFO_PATH))
        val info = response.body() ?: throw AuthException(
            response.serverMessage()?.let(UiText::Raw) ?: UiText.Res(R.string.error_not_voltis, response.code()),
        )
        if (info.apiVersion < MIN_API_VERSION || info.serverId.isBlank()) {
            throw AuthException(
                if (info.version.isBlank()) UiText.Res(R.string.error_old_server) else UiText.Res(R.string.error_old_server_version, info.version),
            )
        }
        val final = response.raw().request.url
        if (url.isHttps && !final.isHttps) throw AuthException(UiText.Res(R.string.error_downgrade))
        store.connect(info.serverId, ServerUrl.fromInfoUrl(final) ?: url)
    }

    suspend fun passwordLogin(username: String, password: String) {
        val server = currentServer()
        val token = try {
            api.token(TokenRequest(username, password, clientName)).token
        } catch (e: HttpException) {
            if (e.code() == 401) throw AuthException(UiText.Res(R.string.error_credentials))
            throw e
        }
        finishSignIn(server, token)
    }

    /** Persists a new flow before the browser opens, and returns the URL to open. */
    suspend fun startBrowserSignIn(): String {
        val server = currentServer()
        val verifier = Pkce.newVerifier()
        store.saveFlow(PendingFlow(verifier, server.id, System.currentTimeMillis()))
        return server.url.newBuilder()
            .addPathSegments("authorize-app")
            .addQueryParameter("code_challenge", Pkce.challenge(verifier))
            .addQueryParameter("client_name", clientName)
            .build()
            .toString()
    }

    /** Exchanges [code] at the flow's own server, so a server switch mid-flow can't leak it. */
    suspend fun completeBrowserSignIn(code: String) {
        val expired = AuthException(UiText.Res(R.string.error_sign_in_expired))
        val flow = store.takeFlow() ?: throw expired
        if (System.currentTimeMillis() - flow.startedAt > FLOW_MAX_AGE_MS) throw expired
        val server = store.server(flow.serverId) ?: throw expired
        val token = api.exchangeAt(server.url.api("api/auth/token/exchange"), ExchangeRequest(code, flow.verifier)).token
        finishSignIn(server, token)
    }

    /** A cancelled browser sign-in. */
    suspend fun cancelBrowserSignIn() {
        store.takeFlow()
    }

    /**
     * Ends the session on the server if it can, and always locally. The account's downloads and
     * unsent changes stay for its next sign-in, unless [deleteData] (P2 §11): then they go once its
     * store has closed.
     */
    suspend fun logout(deleteData: Boolean = false) = withContext(NonCancellable) {
        // The session the user confirmed: everything below acts on it only, never on a newer one.
        val sent = store.active() ?: return@withContext
        val account = sent.account
        // Tagged with the session, so a request that waits for the dispatcher can't pick up newer credentials when it runs.
        if (account != null) runCatching { api.logout(session = sent) }
        // The delete intent is stored with the sign-out. A failure here propagates: still signed in.
        if (!store.signOut(sent, deleteIntent = account.takeIf { deleteData })) return@withContext
        // The session has ended: what follows is best effort and must not fail the caller.
        bestEffort("clear the image cache") { clearImages() }
        if (deleteData && account != null) {
            bestEffort("delete the account's data") {
                if (stores.deleteClosed(account) { store.generationOf(sent.server.id) == sent.generation }) store.clear(account)
            }
        }
    }

    private suspend fun bestEffort(what: String, block: suspend () -> Unit) {
        try {
            block()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Log.e(TAG, "Couldn't $what", e)
        }
    }

    /** Also drops a pending browser flow, so a late callback can't reach whatever is entered next. */
    suspend fun changeServer() {
        store.takeFlow()
        // Before the state changes: that disposes the scope this runs in.
        clearImages()
        store.clearCurrent()
    }

    /** "Google Pixel 8": shown in the server's session list. */
    private val clientName = Build.MODEL.let { if (it.startsWith(Build.MANUFACTURER, true)) it else "${Build.MANUFACTURER} $it" }
        .trim().replaceFirstChar(Char::uppercaseChar).take(64).ifEmpty { "Android" }

    private fun currentServer(): Server = store.active()?.server ?: throw AuthException(UiText.Res(R.string.error_no_server))

    /** Covers and pages are cached per server, not per user. */
    private suspend fun clearImages() = withContext(Dispatchers.IO) {
        images.get().memoryCache?.clear()
        images.get().diskCache?.clear()
    }

    private suspend fun finishSignIn(server: Server, token: String) {
        val me = api.meAt(server.url.api("api/users/me"), "Bearer $token")
        store.signIn(server.id, token, me.id, me.username)
    }

    private companion object {
        const val TAG = "AuthRepository"
        // The web login can take a while; the server's 2-minute code only starts at Continue.
        const val FLOW_MAX_AGE_MS = 30 * 60 * 1000L
    }
}
