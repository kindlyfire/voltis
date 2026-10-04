package me.tijlvdb.voltis.data.api

import kotlinx.coroutines.runBlocking
import me.tijlvdb.voltis.data.auth.SessionStore
import okhttp3.Interceptor
import okhttp3.Response

/**
 * Adds the stored bearer only for the stored server's origin, so a probe of another URL never
 * carries it. Must run after [ServerUrlInterceptor] to see the real host.
 */
class AuthInterceptor(private val store: SessionStore) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        val active = store.active()
        val token = active?.token
        if (token == null || request.header("Authorization") != null ||
            !request.url.sameOrigin(active.server.url)
        ) {
            return chain.proceed(request)
        }
        val response = chain.proceed(
            request.newBuilder().header("Authorization", "Bearer $token").build(),
        )
        // Runs on an OkHttp thread; blocking keeps the state change ordered before the response.
        if (response.code == 401) runBlocking { store.markNeedsReauth(token) }
        return response
    }
}
