package me.tijlvdb.voltis.data.downloads

import android.content.Context
import android.content.pm.ServiceInfo
import android.os.Build
import android.util.Log
import androidx.hilt.work.HiltWorker
import androidx.work.CoroutineWorker
import androidx.work.ForegroundInfo
import androidx.work.WorkerParameters
import dagger.assisted.Assisted
import dagger.assisted.AssistedInject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withTimeoutOrNull
import me.tijlvdb.voltis.data.api.PLACEHOLDER_HOST
import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.domain.downloads.DownloadState
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.reading.ReadingSync
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import okhttp3.HttpUrl
import okhttp3.OkHttpClient

/**
 * Runs the queue of the account it was started for (P2 §8, Worker): one item at a time, oldest
 * first, until none is left. In front with its notification when the system allows it, else as a
 * plain worker posting the same notification.
 */
@HiltWorker
class DownloadWorker @AssistedInject constructor(
    @Assisted context: Context,
    @Assisted params: WorkerParameters,
    private val downloads: DownloadRepository,
    private val client: OkHttpClient,
    private val reading: ReadingSync,
    private val connectivity: Connectivity,
    private val notifier: DownloadNotifier,
) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val accountDir = inputData.getString(ACCOUNT) ?: return Result.success()
        // At process start the store opens a moment after the worker may start.
        val store = withTimeoutOrNull(10_000) { downloads.current.first { it?.accountStore?.dir?.name == accountDir } } ?: return Result.success()
        // Not posted yet: nothing to take down if the queue turns out empty.
        var foreground = true
        try {
            // Through the store: its start requeues what a killed process left running.
            val first = store.head() ?: return Result.success()
            foreground = try {
                setForeground(foregroundInfo(first, read(store) { content().get(first.contentId)?.title }, accountDir))
                true
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                // Android 12+ refuses a foreground service from the background (ForegroundServiceStartNotAllowedException).
                Log.i(TAG, "Running as a plain worker", e)
                false
            }
            while (true) {
                var title: String? = null
                val ran = store.runNext { run ->
                    title = read(store) { content().get(run.contentId)?.title }
                    var shown = 0L
                    notifier.progress(accountDir, run.contentId, run.dir.name, run.pagesDone, run.pageCount, title)
                    val transfer = Transfer(
                        store, client, BASE,
                        pageCount = reading::pageCount,
                        freeBytes = DownloadRepository::freeBytes,
                        onProgress = {
                            val now = System.currentTimeMillis()
                            if (now - shown >= PROGRESS_INTERVAL || it.pagesDone == it.pageCount) {
                                shown = now
                                notifier.progress(accountDir, it.contentId, it.transferId ?: run.dir.name, it.pagesDone, it.pageCount, title)
                            }
                        },
                        onRestart = { notifier.progress(accountDir, it.contentId, it.dir.name, it.pagesDone, it.pageCount, title) },
                        unreachable = connectivity::unreachable,
                    )
                    transfer.run(run)
                } ?: return Result.success()
                ran.row?.takeIf { it.state == DownloadState.FAILED }?.let { notifier.failed(it, title) }
                when (ran.result) {
                    Transfer.Outcome.RETRY -> return Result.retry()
                    Transfer.Outcome.STOP -> return Result.success()
                    else -> continue
                }
            }
        } catch (e: SyncUnavailable) {
            // Signed out, switched, or the downloads couldn't be opened: nothing to run for this account.
            return Result.success()
        } catch (e: DownloadsPersistenceFailed) {
            // An item's end, or the repair and claim before it, couldn't be recorded (a full disk): it would head the queue again at once.
            Log.e(TAG, "Couldn't record a download's state", e)
            return if (runAttemptCount < MAX_PERSISTENCE_RETRIES) Result.retry() else Result.failure()
        } finally {
            if (!foreground) notifier.cancelProgress()
        }
    }

    private fun foregroundInfo(row: DownloadEntity, title: String?, accountDir: String): ForegroundInfo {
        val notification = notifier.progressNotification(accountDir, row.contentId, row.transferId.orEmpty(), row.pagesDone, row.pageCount, title)
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            ForegroundInfo(DownloadNotifier.PROGRESS_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
        } else {
            ForegroundInfo(DownloadNotifier.PROGRESS_ID, notification)
        }
    }

    /** Null also when the store closed meanwhile. */
    private suspend fun <T> read(store: DownloadStore, block: suspend VoltisDatabase.() -> T?): T? = try {
        store.accountStore.db.block()
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        null
    }

    companion object {
        /** Runs after a failed record (an end, a repair or a claim): then the work fails, until the next start replaces it. */
        const val MAX_PERSISTENCE_RETRIES = 3

        /** Input: the account's directory name, not the account itself. */
        const val ACCOUNT = "account"
        private const val TAG = "DownloadWorker"
        private const val PROGRESS_INTERVAL = 500L
        private val BASE = HttpUrl.Builder().scheme("http").host(PLACEHOLDER_HOST).build()
    }
}
