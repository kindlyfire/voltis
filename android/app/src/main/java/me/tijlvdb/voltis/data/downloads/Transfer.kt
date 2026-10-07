package me.tijlvdb.voltis.data.downloads

import android.graphics.BitmapFactory
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ForAccount
import me.tijlvdb.voltis.data.api.TimeoutInterceptor
import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.data.net.Missing
import me.tijlvdb.voltis.data.net.VoltisAnswer
import me.tijlvdb.voltis.domain.downloads.DownloadError
import me.tijlvdb.voltis.domain.downloads.DownloadState
import me.tijlvdb.voltis.domain.downloads.ErrorKind
import me.tijlvdb.voltis.domain.storage.StorageFullException
import okhttp3.HttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okio.BufferedSink
import okio.BufferedSource
import okio.buffer
import okio.sink

/**
 * One claimed item's steps (P2 §8, One item): its detail row, the resume point, the free-space
 * checks, the offline stream into page files, the page sizes and the covers, all in the run's own
 * directory, then publication. Requests go to [base] (the placeholder host in the app) for the
 * run's account. Every result goes through [store], which drops it once the run was revoked.
 */
class Transfer(
    private val store: DownloadStore,
    private val client: OkHttpClient,
    private val base: HttpUrl,
    /** The reading sync's page count of an item of an account, once it is downloaded. */
    private val pageCount: suspend (account: String, contentId: String, count: Int) -> Unit,
    private val freeBytes: (File) -> Long,
    private val onProgress: (DownloadEntity) -> Unit = {},
    /** The run goes on in a new directory: what names its transfer (the notification's actions) is out of date. */
    private val onRestart: (Run) -> Unit = {},
    private val unreachable: () -> Unit = {},
    private val clock: () -> Long = System::currentTimeMillis,
) {
    /** What the worker does next: the next item, a `retry` (no connection), or stop (signed out). */
    enum class Outcome { NEXT, RETRY, STOP }

    /** A report wasn't admitted: what is under way is dropped. */
    private class Stopped : Exception()

    private val db = store.accountStore.db

    suspend fun run(claimed: Run): Outcome = withContext(Dispatchers.IO) {
        detail(claimed)
        var run = resumable(claimed) ?: return@withContext Outcome.NEXT
        var mayRestart = true
        var outcome: Outcome? = null
        try {
            while (outcome == null) {
                outcome = attempt(run, mayRestart)
                if (outcome == null) {
                    // A 400 for `from > 0`, or a new version: once per run, from page 0 in a new directory.
                    mayRestart = false
                    run = restart(run) ?: return@withContext Outcome.NEXT
                }
            }
            outcome
        } catch (e: Stopped) {
            Outcome.NEXT
        }
    }

    /** Step 0, best effort: a list row is replaced by the detail row, and a series not cached yet is fetched. */
    private suspend fun detail(run: Run) {
        val content = db.content()
        val cached = content.get(run.contentId)
        val series = if (cached?.detail == true) cached.parentId else fetch(run, run.contentId)?.parentId ?: cached?.parentId
        if (series != null && content.get(series) == null) fetch(run, series)
    }

    /** The detail row of [id], cached; null when it can't be fetched. */
    private suspend fun fetch(run: Run, id: String): Content? = try {
        val fetched = get(run, url("api/content", id)) { r ->
            if (r.isSuccessful) AppJson.decodeFromString(Content.serializer(), r.body.string()) else null
        }
        fetched?.also { db.cache(listOf(it to null), clock()) { true } }
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        // The next refresh or the item's own page fills it in.
        null
    }

    /** The run as it resumes: `from=N` resumes only a directory holding the manifest of its version and pages 0 to N-1. */
    private suspend fun resumable(run: Run): Run? {
        run.dir.listFiles { f -> f.name.endsWith(".part") }?.forEach { it.delete() }
        if (run.version != null && manifestIn(run.dir)?.version == run.version && (0 until run.pagesDone).all { File(run.dir, "$it").isFile }) return run
        if (run.pagesDone == 0 && run.dir.list().isNullOrEmpty()) return run
        return restart(run)
    }

    private suspend fun restart(run: Run): Run? = store.restart(run)?.also(onRestart)

    /** One request from the run's resume point. Null asks for a restart from page 0. */
    private suspend fun attempt(run: Run, mayRestart: Boolean): Outcome? {
        val from = run.pagesDone
        val left = if (from > 0) (run.fileSize ?: 0L) - run.bytesDone else run.cachedFileSize ?: 0L
        if (!fits(run, left)) return storageFull(run, DownloadError.NO_SPACE)
        val request = request(run, url("api/files/offline", run.contentId).newBuilder().addQueryParameter("from", from.toString()).build())
            .newBuilder().header(TimeoutInterceptor.HEADER, "120").build()
        // The observation time of what this request sees: its manifest, or the item gone.
        val at = clock()
        return try {
            call(request) { transfer(it, run, from, at, mayRestart) }
        } catch (e: IOException) {
            // No answer: the stream's own failures are handled in transfer().
            currentCoroutineContext().ensureActive()
            waitForNetwork(run)
        }
    }

    private suspend fun transfer(r: Response, run: Run, from: Int, at: Long, mayRestart: Boolean): Outcome? {
        if (!r.isSuccessful) return status(run, from, at, r, mayRestart)
        // Only the stream: another 2xx (a proxy's JSON, a portal) is no answer from Voltis.
        if (r.body.contentType()?.let { it.type == "application" && it.subtype == "vnd.voltis.pages" } != true) return waitForNetwork(run)
        val sink = Sink(run, from, at)
        try {
            readOfflineStream(r.body.source(), from, sink)
        } catch (e: OfflineError.Page) {
            return fail(run, ErrorKind.PAGE, e.message)
        } catch (e: OfflineError.Protocol) {
            return fail(run, ErrorKind.PROTOCOL, DownloadError.PROTOCOL, e.message)
        } catch (e: OfflineError.Truncated) {
            currentCoroutineContext().ensureActive()
            unreachable()
            val row = store.end(run, RunEnd.BrokenOff(sink.stored)) ?: return Outcome.NEXT
            // A failed item leaves the others to go on now, not after a backoff.
            return if (row.state == DownloadState.FAILED) Outcome.NEXT else Outcome.RETRY
        } catch (e: IOException) {
            // A write or sync that failed.
            currentCoroutineContext().ensureActive()
            return storageFull(run, DownloadError.WRITE_FAILED)
        } catch (e: NoSpace) {
            return storageFull(run, DownloadError.NO_SPACE)
        } catch (e: StorageFullException) {
            return storageFull(run, DownloadError.NO_SPACE)
        }
        if (sink.changed) return null
        try {
            complete(run)
        } catch (e: IOException) {
            currentCoroutineContext().ensureActive()
            return storageFull(run, DownloadError.WRITE_FAILED)
        }
        return Outcome.NEXT
    }

    /** Step 4: an answer that isn't the stream. */
    private suspend fun status(run: Run, from: Int, at: Long, response: Response, mayRestart: Boolean): Outcome? {
        val answer = VoltisAnswer.of(response) as? VoltisAnswer.Genuine ?: return waitForNetwork(run)
        return when (answer.code) {
            400 -> if (from > 0 && mayRestart) null else fail(run, ErrorKind.SERVER, answer.message)
            404 -> when (answer.message) {
                Missing.CONTENT, Missing.FILE -> {
                    store.end(run, RunEnd.Gone(at))
                    Outcome.NEXT
                }
                // Not a comic with pages: nothing to download, but not gone either.
                "Content has no pages" -> fail(run, ErrorKind.SERVER, answer.message)
                // A route or a proxy, not the item.
                else -> waitForNetwork(run)
            }
            409 -> fail(run, ErrorKind.SERVER, DownloadError.FILE_CHANGED)
            // Stays queued: the queue starts again when the session is back.
            401 -> if (store.end(run, RunEnd.Unauthorized) != null) Outcome.STOP else Outcome.NEXT
            in 500..599 -> {
                store.end(run, RunEnd.ServerError(answer.message))
                Outcome.NEXT
            }
            else -> fail(run, ErrorKind.SERVER, answer.message)
        }
    }

    private suspend fun waitForNetwork(run: Run): Outcome {
        unreachable()
        return if (store.end(run, RunEnd.Wait) != null) Outcome.RETRY else Outcome.NEXT
    }

    private suspend fun fail(run: Run, kind: String, error: String?, detail: String? = null): Outcome {
        store.end(run, RunEnd.Failed(kind, error, detail))
        return Outcome.NEXT
    }

    /** The item fails and the queue pauses: nothing else would fit either. */
    private suspend fun storageFull(run: Run, error: String): Outcome {
        store.end(run, RunEnd.StorageFull(error))
        return Outcome.NEXT
    }

    /** [bytes] more, and a margin, fit beside what is stored. */
    private fun fits(run: Run, bytes: Long) = freeBytes(run.dir) >= bytes.coerceAtLeast(0) + SPARE_BYTES

    /** Writes pages as they arrive, each synced and renamed into place before it is reported. */
    private inner class Sink(private val run: Run, from: Int, private val at: Long) : OfflineSink {
        /** The version differs from the pages already stored. */
        var changed = false
            private set

        /** This run stored at least one page. */
        var stored = false
            private set

        private val resumed = from > 0

        override suspend fun manifest(manifest: OfflineManifest): Boolean {
            if (resumed && manifest.version != run.version) {
                changed = true
                return false
            }
            if (!resumed) {
                if (!fits(run, manifest.fileSize ?: 0)) throw NoSpace()
                writeSynced(File(run.dir, MANIFEST)) { it.writeUtf8(AppJson.encodeToString(OfflineManifest.serializer(), manifest)) }
            }
            onProgress(store.manifest(run, manifest, at) ?: throw Stopped())
            return true
        }

        override suspend fun page(index: Int, length: Long, data: BufferedSource) {
            if (!fits(run, length)) throw NoSpace()
            writeSynced(File(run.dir, "$index"), length) { it.writeAll(data) }
            stored = true
            onProgress(store.page(run, index, length) ?: throw Stopped())
        }
    }

    /** Free space ran out before a write. Not an IOException: the stream reading it isn't cut off. */
    private class NoSpace : Exception()

    /** After the last page: the sizes the manifest lacks, the covers, then the copy is published. */
    private suspend fun complete(run: Run) {
        withSizes(run.dir)
        val content = db.content()
        val seriesCover = run.seriesId != run.contentId &&
            cover(run, run.seriesId, content.get(run.seriesId)?.coverVersion, File(run.dir, SERIES_COVER))
        val cover = cover(run, run.contentId, content.get(run.contentId)?.coverVersion, File(run.dir, COVER))
        val bytes = run.dir.listFiles()?.sumOf { it.length() } ?: 0
        if (!store.publish(run, CopyMade(bytes, cover, seriesCover))) return
        val count = manifestIn(run.dir)?.pageCount ?: return
        try {
            pageCount(run.account, run.contentId, count)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // The lane learns it from the reader instead.
        }
    }

    /**
     * Sizes the server didn't have (it sizes pages on the first `page_sizes=1` request), read from
     * the pages' headers into this directory's manifest, once: the reader's Auto mode and spreads need them.
     */
    private fun withSizes(dir: File) {
        val manifest = manifestIn(dir) ?: return
        if (manifest.pages.all { it.width != null && it.height != null }) return
        val sized = manifest.copy(
            pages = manifest.pages.mapIndexed { i, page ->
                if (page.width != null && page.height != null) return@mapIndexed page
                val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
                BitmapFactory.decodeFile(File(dir, "$i").path, bounds)
                if (bounds.outWidth > 0 && bounds.outHeight > 0) page.copy(width = bounds.outWidth, height = bounds.outHeight) else page
            },
        )
        writeSynced(File(dir, MANIFEST)) { it.writeUtf8(AppJson.encodeToString(OfflineManifest.serializer(), sized)) }
    }

    /** Best effort: a fallback (`no-store`) or a failure stores nothing. */
    private suspend fun cover(run: Run, id: String, version: String?, file: File): Boolean {
        if (version == null) return false
        currentCoroutineContext().ensureActive()
        val url = url("api/files/cover", id).newBuilder().addQueryParameter("v", version).build()
        return try {
            get(run, url) { r ->
                val image = r.body.contentType()?.type == "image"
                if (!r.isSuccessful || !image || r.header("Cache-Control")?.contains("no-store") == true) return@get false
                writeSynced(file) { it.writeAll(r.body.source()) }
                true
            }
        } catch (e: IOException) {
            false
        }
    }

    private fun url(path: String, id: String) = base.newBuilder().addPathSegments(path).addPathSegment(id).build()

    private fun request(run: Run, url: HttpUrl) = Request.Builder().url(url).tag(ForAccount::class.java, ForAccount(run.account)).build()

    private suspend fun <T> get(run: Run, url: HttpUrl, block: (Response) -> T): T = call(request(run, url)) { block(it) }

    /** Executes [request] and reads its response with [block]; this coroutine's cancellation cancels the call, also while its body is read. */
    private suspend fun <T> call(request: Request, block: suspend (Response) -> T): T = coroutineScope {
        val call = client.newCall(request)
        // Undispatched: it waits before execute() can block, so a cancellation at any moment, also one from before, cancels the call.
        val watcher = launch(start = CoroutineStart.UNDISPATCHED) {
            try {
                awaitCancellation()
            } finally {
                call.cancel()
            }
        }
        try {
            call.execute().use { block(it) }
        } finally {
            watcher.cancel()
        }
    }

    private companion object {
        /** Kept free beyond what the item still needs. */
        const val SPARE_BYTES = 50L shl 20
    }
}

/** The manifest in [dir], or null when there is none that parses. */
internal fun manifestIn(dir: File): OfflineManifest? = try {
    AppJson.decodeFromString(OfflineManifest.serializer(), File(dir, MANIFEST).readText())
} catch (e: Exception) {
    null
}

/** Written to `<file>.part`, synced to the disk, checked to be [length] bytes if given, then renamed: a name that exists always has its bytes. */
internal fun writeSynced(file: File, length: Long? = null, write: (BufferedSink) -> Unit) {
    val part = File(file.path + ".part")
    try {
        FileOutputStream(part).use { out ->
            val sink = out.sink().buffer()
            write(sink)
            sink.flush()
            out.fd.sync()
        }
        if (length != null && part.length() != length) throw IOException("${file.name}: ${part.length()} of $length bytes written")
        if (!part.renameTo(file)) throw IOException("Couldn't store ${file.name}")
    } catch (e: Throwable) {
        part.delete()
        throw e
    }
}
