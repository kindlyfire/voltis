package me.tijlvdb.voltis.data.reader

import coil3.ImageLoader
import coil3.disk.DiskCache
import java.io.File
import java.io.IOException
import java.util.concurrent.ConcurrentHashMap
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import me.tijlvdb.voltis.data.images.ServerKeyInterceptor
import me.tijlvdb.voltis.data.storage.AndroidStorageSpace
import me.tijlvdb.voltis.domain.storage.LOW_SPACE
import me.tijlvdb.voltis.domain.storage.StorageFullException
import okhttp3.Call
import okhttp3.HttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request

/** Fetches pages into Coil's disk cache without decoding them. */
@Singleton
class PagePreloader @Inject constructor(
    private val client: OkHttpClient,
    private val images: ImageLoader,
    private val serverKey: ServerKeyInterceptor,
) {
    private val inFlight = ConcurrentHashMap<String, CompletableDeferred<Unit>>()

    /**
     * Coil's network fetcher serves a disk entry with empty metadata as is, so a later request
     * for [url] reads the file and makes no network call. Best effort: a failed page is simply
     * not stored. Returns once the page is stored or has failed, so a caller that waits and then
     * asks Coil never downloads beside this.
     */
    suspend fun toDisk(url: HttpUrl) {
        val cache = images.diskCache ?: return
        val key = serverKey.diskKey(url.toString())
        // A second request for a page waits for the first and shares its outcome.
        val done = CompletableDeferred<Unit>()
        inFlight.putIfAbsent(key, done)?.let { return it.await() }
        try {
            withContext(Dispatchers.IO) { store(cache, key, client.newCall(Request(url))) }
        } finally {
            inFlight.remove(key)
            done.complete(Unit)
        }
    }

    /** Reads the stored file of [url], fetching it first if needed. Throws when the page isn't there: [StorageFullException] when the cache's disk is full. */
    suspend fun <T> read(url: HttpUrl, block: (File) -> T): T {
        toDisk(url)
        return withContext(Dispatchers.IO) {
            val cache = images.diskCache
            val snapshot = cache?.openSnapshot(serverKey.diskKey(url.toString()))
                ?: throw if (cache != null && AndroidStorageSpace.free(cache.directory.toFile()) < LOW_SPACE) StorageFullException() else IOException("Page not stored")
            snapshot.use { block(it.data.toFile()) }
        }
    }

    /**
     * Blocking, so the cache entry is let go, and waiters are woken, only once the copy has
     * stopped. Cancelling the scope cancels the call, which fails the request or the copy.
     */
    private fun CoroutineScope.store(cache: DiskCache, key: String, call: Call) {
        // Undispatched, so it is waiting before anything can cancel it.
        val canceller = launch(start = CoroutineStart.UNDISPATCHED) {
            try {
                awaitCancellation()
            } finally {
                call.cancel()
            }
        }
        try {
            cache.openSnapshot(key)?.let { return it.close() }
            val editor = cache.openEditor(key) ?: return
            var stored = false
            try {
                call.execute().use { response ->
                    if (response.isSuccessful) {
                        cache.fileSystem.write(editor.data) { writeAll(response.body.source()) }
                        stored = true
                    }
                }
            } finally {
                if (stored) editor.commit() else editor.abort()
            }
        } catch (_: IOException) {
            // The request failed or was cancelled, or the cache couldn't be read or written.
        } finally {
            canceller.cancel()
        }
    }
}
