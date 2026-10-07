package me.tijlvdb.voltis.data.reading

import android.content.Context
import androidx.work.BackoffPolicy
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.workDataOf
import dagger.hilt.android.qualifiers.ApplicationContext
import java.util.concurrent.TimeUnit
import javax.inject.Inject
import javax.inject.Singleton
import me.tijlvdb.voltis.data.net.anyNetwork

/** Asks for a background drain of the outbox (P2 §5, Background drain). */
fun interface SyncScheduler {
    fun schedule()
}

/**
 * Two unique works per account, both [SyncWorker]s for the account's directory, so accounts never
 * share or cancel each other's work:
 * - `reading-sync-<dir>`, one-time, asked for by the engine: `KEEP`, so a work that waits (for a
 *   network, or in its backoff) stays and the backoff spaces the tries, also across a restart.
 *   `KEEP` drops an ask made while that work is running or finishing, and nothing here tracks it:
 * - `reading-sync-periodic-<dir>`, every 15 minutes, enqueued when the account's engine opens (`KEEP`),
 *   is the safety net: it drains when the outbox has something, so a dropped ask waits for its next
 *   eligible run (nominally 15 minutes, subject to constraints and OS scheduling; the foreground drains as usual).
 * Both are cancelled by [close] before the account's store closes. Only the open account's engine schedules.
 */
@Singleton
class SyncWork internal constructor(
    /** `KEEP` the unique work [name], a [SyncWorker] for [accountDir], once or every [PERIOD_MINUTES] minutes. */
    private val enqueue: (name: String, accountDir: String, periodic: Boolean) -> Unit,
    private val cancel: (name: String) -> Unit,
) {
    @Inject constructor(@ApplicationContext context: Context) : this(
        enqueue = { name, dir, periodic ->
            val input = workDataOf(SyncWorker.ACCOUNT to dir, SyncWorker.PERIODIC to periodic)
            val manager = WorkManager.getInstance(context)
            if (periodic) {
                val request = PeriodicWorkRequestBuilder<SyncWorker>(PERIOD_MINUTES, TimeUnit.MINUTES)
                    .setConstraints(anyNetwork())
                    .setInputData(input)
                    .build()
                manager.enqueueUniquePeriodicWork(name, ExistingPeriodicWorkPolicy.KEEP, request)
            } else {
                val request = OneTimeWorkRequestBuilder<SyncWorker>()
                    .setConstraints(anyNetwork())
                    .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS)
                    .setInputData(input)
                    .build()
                manager.enqueueUniqueWork(name, ExistingWorkPolicy.KEEP, request)
            }
        },
        cancel = { WorkManager.getInstance(context).cancelUniqueWork(it) },
    )

    /** WorkManager can be used: the application has its worker factory. */
    private var begun = false

    /** Asked for before [begin]. */
    private var pending: String? = null

    /** The open account's directory, whose engine may schedule. */
    private var open: String? = null

    /** Called once the application is injected; what was asked for earlier is enqueued now. */
    @Synchronized
    fun begin() {
        begun = true
        open?.let { enqueue(periodicName(it), it, true) }
        pending?.takeIf { it == open }?.let { enqueue(workName(it), it, false) }
        pending = null
    }

    /**
     * The scheduler of [accountDir]'s engine, or of its pending owner, which does nothing once [close] has
     * closed it. Idempotent while [accountDir] is open: the engine and the owner share its two works.
     */
    @Synchronized
    fun open(accountDir: String): SyncScheduler {
        if (open != accountDir) {
            open = accountDir
            if (begun) enqueue(periodicName(accountDir), accountDir, true)
        }
        return SyncScheduler { schedule(accountDir) }
    }

    @Synchronized
    private fun schedule(accountDir: String) {
        if (accountDir != open) return
        if (begun) enqueue(workName(accountDir), accountDir, false) else pending = accountDir
    }

    /** [accountDir]'s store is closing: nothing more is scheduled for it, and both its works are cancelled. */
    @Synchronized
    fun close(accountDir: String) {
        if (open == accountDir) open = null
        if (pending == accountDir) pending = null
        if (begun) {
            cancel(workName(accountDir))
            cancel(periodicName(accountDir))
        }
    }

    companion object {
        const val PERIOD_MINUTES = 15L

        fun workName(accountDir: String) = "reading-sync-$accountDir"

        fun periodicName(accountDir: String) = "reading-sync-periodic-$accountDir"
    }
}
