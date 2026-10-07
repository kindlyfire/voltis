package me.tijlvdb.voltis.data.api

import java.io.IOException
import me.tijlvdb.voltis.data.auth.SessionStore
import okhttp3.Interceptor
import okhttp3.Response

/** The account a request is made for. It is refused before it is sent while another one is signed in. */
data class ForAccount(val account: String)

/** A request's tag: `Connectivity`'s probe, which reports on reachability itself and carries no credentials. */
object ConnectivityProbe

/** A request made for an account that isn't the signed-in one: nothing was sent. */
class AccountChangedException : IOException("Signed in to another account")

/**
 * Points requests for [PLACEHOLDER_HOST] at the current server; others pass through untouched.
 * The session it read is tagged on the request, so [AuthInterceptor] adds that one's credentials.
 */
class ServerUrlInterceptor(private val store: SessionStore) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        if (request.url.host != PLACEHOLDER_HOST) return chain.proceed(request)
        // A request made for one session (a sign-out) keeps it, though another is current by the time it runs.
        val preset = request.tag(SessionStore.Active::class.java)
        val active = preset ?: store.active() ?: throw IOException("No server selected")
        if (preset == null) request.tag(ForAccount::class.java)?.let { if (it.account != active.account) throw AccountChangedException() }
        val base = active.server.url
        val url = request.url.newBuilder()
            .scheme(base.scheme)
            .host(base.host)
            .port(base.port)
            .encodedPath(base.encodedPath.trimEnd('/') + request.url.encodedPath)
            .build()
        return chain.proceed(request.newBuilder().url(url).tag(SessionStore.Active::class.java, active).build())
    }
}
