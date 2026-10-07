package me.tijlvdb.voltis.data.net

import java.io.IOException
import me.tijlvdb.voltis.data.api.ConnectivityProbe
import me.tijlvdb.voltis.data.api.sameOrigin
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.domain.net.Connectivity
import okhttp3.Interceptor
import okhttp3.Response

/**
 * Tells [Connectivity] how each request to the current server went (P2 §10): a genuine answer, or
 * none. It changes no response. A cancelled call, the probe itself and a late answer from a server that is
 * no longer current report nothing. Placed after `ServerUrlInterceptor`, so it sees the real origin.
 */
class ReachabilityInterceptor(private val store: SessionStore, private val connectivity: () -> Connectivity) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        val server = (request.tag(SessionStore.Active::class.java) ?: store.active())?.server
        if (server == null || !request.url.sameOrigin(server.url) || request.tag(ConnectivityProbe::class.java) != null) {
            return chain.proceed(request)
        }
        fun current() = store.active()?.server?.id == server.id
        val response = try {
            chain.proceed(request)
        } catch (e: IOException) {
            if (!chain.call().isCanceled() && current()) connectivity().unreachable()
            throw e
        }
        if (current()) {
            when (VoltisAnswer.of(response)) {
                is VoltisAnswer.Genuine -> connectivity().answered()
                VoltisAnswer.Unreachable -> connectivity().unreachable()
            }
        }
        return response
    }
}
