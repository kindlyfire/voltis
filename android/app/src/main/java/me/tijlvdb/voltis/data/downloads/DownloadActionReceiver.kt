package me.tijlvdb.voltis.data.downloads

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.data.auth.AppScope

/** The notification's Pause and Cancel, for the account and transfer it was posted for (its data URI, `DownloadAction`). */
@AndroidEntryPoint
class DownloadActionReceiver : BroadcastReceiver() {
    @Inject
    lateinit var downloads: DownloadRepository

    @Inject
    @AppScope
    lateinit var scope: CoroutineScope

    override fun onReceive(context: Context, intent: Intent) {
        val uri = intent.data ?: return
        val pending = goAsync()
        scope.launch {
            try {
                downloads.fromNotification(uri)
            } finally {
                pending.finish()
            }
        }
    }
}
