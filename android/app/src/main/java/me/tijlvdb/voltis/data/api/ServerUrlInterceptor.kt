package me.tijlvdb.voltis.data.api

import java.io.IOException
import me.tijlvdb.voltis.data.auth.SessionStore
import okhttp3.Interceptor
import okhttp3.Response

/** Points requests for [PLACEHOLDER_HOST] at the current server; others pass through untouched. */
class ServerUrlInterceptor(private val store: SessionStore) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        if (request.url.host != PLACEHOLDER_HOST) return chain.proceed(request)
        val base = store.active()?.server?.url ?: throw IOException("No server selected")
        val url = request.url.newBuilder()
            .scheme(base.scheme)
            .host(base.host)
            .port(base.port)
            .encodedPath(base.encodedPath.trimEnd('/') + request.url.encodedPath)
            .build()
        return chain.proceed(request.newBuilder().url(url).build())
    }
}
