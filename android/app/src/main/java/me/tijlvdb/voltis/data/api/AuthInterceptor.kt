package me.tijlvdb.voltis.data.api

import kotlinx.coroutines.runBlocking
import me.tijlvdb.voltis.data.auth.Server
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.net.VoltisAnswer
import me.tijlvdb.voltis.domain.storage.StorageFullException
import okhttp3.Interceptor
import okhttp3.Response

/**
 * Adds the stored bearer only for the stored server's origin, so a probe of another URL never
 * carries it. Must run after [ServerUrlInterceptor] to see the real host.
 *
 * A 401 drops the token only when it is Voltis' own answer to that bearer and [probe] then finds that
 * server there, still current (P2 §10): a captive portal or another device at the address must not sign the
 * user out, and their offline data can't be read from the Login screen.
 */
class AuthInterceptor(private val store: SessionStore, private val probe: suspend (expected: Server) -> Boolean) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        // The session the URL was made from, so the server and the credentials can't come from two.
        val active = request.tag(SessionStore.Active::class.java) ?: store.active()
        val token = active?.token
        if (token == null || request.header("Authorization") != null ||
            !request.url.sameOrigin(active.server.url) || request.tag(ConnectivityProbe::class.java) != null
        ) {
            return chain.proceed(request)
        }
        val bearer = "Bearer $token"
        val response = chain.proceed(request.newBuilder().header("Authorization", bearer).build())
        if (response.code != 401) return response
        // OkHttp follows redirects below this interceptor: the 401 must answer the bearer, at its own origin.
        val final = response.request
        val ours = final.header("Authorization") == bearer && final.url.sameOrigin(active.server.url)
        // Runs on an OkHttp thread; blocking keeps the state change ordered before the response.
        if (ours && VoltisAnswer.of(response) is VoltisAnswer.Genuine && runBlocking { probe(active.server) }) {
            try {
                runBlocking { store.markNeedsReauth(active) }
            } catch (_: StorageFullException) {
                // Not stored: the next 401 tries again.
            }
        }
        return response
    }
}
