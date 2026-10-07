package me.tijlvdb.voltis.data.downloads

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject
import javax.inject.Singleton
import me.tijlvdb.voltis.MainActivity
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.ui.nav.LaunchTarget

/** The download's notification (P2 §8): progress with Pause and Cancel, and one "Download failed". Both open Downloads. */
@Singleton
class DownloadNotifier @Inject constructor(@ApplicationContext private val context: Context) {
    private val manager = context.getSystemService(NotificationManager::class.java)

    init {
        manager.createNotificationChannel(
            NotificationChannel(CHANNEL, context.getString(R.string.downloads_title), NotificationManager.IMPORTANCE_LOW),
        )
    }

    /** The actions are for [contentId]'s transfer [transferId], of the account whose directory is [accountDir]. */
    fun progressNotification(accountDir: String, contentId: String, transferId: String, pagesDone: Int, pageCount: Int?, title: String?): Notification {
        val total = pageCount ?: 0
        return NotificationCompat.Builder(context, CHANNEL)
            .setSmallIcon(R.drawable.ic_download)
            .setContentTitle(title ?: context.getString(R.string.downloads_title))
            .setContentText(if (total > 0) context.getString(R.string.downloads_page_of, pagesDone, total) else context.getString(R.string.downloads_starting))
            .setProgress(total, pagesDone, total == 0)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setSilent(true)
            .setContentIntent(openDownloads())
            .addAction(0, context.getString(R.string.downloads_pause), action(DownloadAction(accountDir, contentId, transferId, cancel = false)))
            .addAction(0, context.getString(R.string.cancel), action(DownloadAction(accountDir, contentId, transferId, cancel = true)))
            .build()
    }

    fun progress(accountDir: String, contentId: String, transferId: String, pagesDone: Int, pageCount: Int?, title: String?) {
        if (allowed()) manager.notify(PROGRESS_ID, progressNotification(accountDir, contentId, transferId, pagesDone, pageCount, title))
    }

    fun cancelProgress() = manager.cancel(PROGRESS_ID)

    /** One notification for the latest failure. */
    fun failed(row: DownloadEntity, title: String?) {
        if (!allowed()) return
        val notification = NotificationCompat.Builder(context, CHANNEL)
            .setSmallIcon(R.drawable.ic_download)
            .setContentTitle(context.getString(R.string.downloads_failed_notification))
            .setContentText(title)
            .setAutoCancel(true)
            .setContentIntent(openDownloads())
            .build()
        manager.notify(FAILED_ID, notification)
    }

    private fun allowed() = Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
        ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED

    private fun openDownloads(): PendingIntent {
        val intent = Intent(context, MainActivity::class.java)
            .setAction(LaunchTarget.ACTION_DOWNLOADS)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        return PendingIntent.getActivity(context, 0, intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    }

    /** Its data URI is its identity: PendingIntent matching ignores extras. */
    private fun action(action: DownloadAction): PendingIntent {
        val intent = Intent(context, DownloadActionReceiver::class.java).setData(action.uri())
        return PendingIntent.getBroadcast(context, 0, intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    }

    companion object {
        const val PROGRESS_ID = 1001
        private const val FAILED_ID = 1002
        private const val CHANNEL = "downloads"
    }
}
