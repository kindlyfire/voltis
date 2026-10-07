package me.tijlvdb.voltis.data.reading

import android.content.Context
import androidx.hilt.work.HiltWorker
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import dagger.assisted.Assisted
import dagger.assisted.AssistedInject
import me.tijlvdb.voltis.data.downloads.DownloadRepository
import me.tijlvdb.voltis.data.sync.SyncCenter
import me.tijlvdb.voltis.domain.reading.DrainResult

/**
 * Drains the outbox and the pending rows of the account it was scheduled for (P2 §5, Background drain; P4 §8), whatever
 * `Connectivity.online` says: in the background nothing refreshes it. Retried with its backoff
 * while ops are left that could go later.
 */
@HiltWorker
class SyncWorker @AssistedInject constructor(
    @Assisted context: Context,
    @Assisted params: WorkerParameters,
    private val sync: SyncCenter,
    private val downloads: DownloadRepository,
) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val account = inputData.getString(ACCOUNT) ?: return Result.success()
        val periodic = inputData.getBoolean(PERIODIC, false)
        val result = sync.drainInBackground(account, periodic)
        // Reading done elsewhere or offline may have moved a series' window. The process may be frozen once this returns.
        val settled = downloads.settle(account)
        // The periodic one is a safety net: its next period is its retry.
        return when {
            periodic -> Result.success()
            result == DrainResult.WAITING || result == DrainResult.BUSY || !settled -> Result.retry()
            // Done, or that account isn't the one signed in: its next engine drains at its start.
            else -> Result.success()
        }
    }

    companion object {
        /** Input: the account's directory name, not the account itself. */
        const val ACCOUNT = "account"

        /** Input: the periodic safety net, which drains only when something is unsent. */
        const val PERIODIC = "periodic"
    }
}
