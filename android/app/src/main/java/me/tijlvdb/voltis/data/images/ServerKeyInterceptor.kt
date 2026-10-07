package me.tijlvdb.voltis.data.images

import coil3.intercept.Interceptor
import coil3.request.ImageResult
import javax.inject.Inject
import javax.inject.Singleton
import me.tijlvdb.voltis.data.auth.SessionStore

/** Puts the current `server_id` in the cache keys of URLs: the placeholder host is the same for every server. A local file is left alone. */
@Singleton
class ServerKeyInterceptor @Inject constructor(private val store: SessionStore) : Interceptor {
    /** The disk cache key of [key] (a URL, unless the request names another) on the current server. */
    fun diskKey(key: String): String = "${store.active()?.server?.id}:$key"

    override suspend fun intercept(chain: Interceptor.Chain): ImageResult {
        val request = chain.request
        if (request.data !is String) return chain.proceed()
        val keyed = request.newBuilder()
            .diskCacheKey(diskKey(request.diskCacheKey ?: request.data.toString()))
            .memoryCacheKeyExtra("server", store.active()?.server?.id)
            .build()
        return chain.withRequest(keyed).proceed()
    }
}
