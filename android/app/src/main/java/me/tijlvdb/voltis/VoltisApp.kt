package me.tijlvdb.voltis

import android.app.Application
import android.content.pm.ApplicationInfo
import android.os.StrictMode
import androidx.hilt.work.HiltWorkerFactory
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.ProcessLifecycleOwner
import androidx.work.Configuration
import coil3.ImageLoader
import coil3.PlatformContext
import coil3.SingletonImageLoader
import dagger.Lazy
import dagger.hilt.android.HiltAndroidApp
import javax.inject.Inject
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.data.auth.AppScope
import me.tijlvdb.voltis.data.auth.SessionState
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.db.AccountStores
import me.tijlvdb.voltis.data.downloads.CatalogRefresher
import me.tijlvdb.voltis.data.downloads.DownloadRepository
import me.tijlvdb.voltis.data.lists.ListsRepository
import me.tijlvdb.voltis.data.net.NetworkConnectivity
import me.tijlvdb.voltis.data.reading.ReadingSyncManager
import me.tijlvdb.voltis.data.reading.SyncWork
import me.tijlvdb.voltis.data.sync.SyncCenter

@HiltAndroidApp
class VoltisApp : Application(), SingletonImageLoader.Factory, Configuration.Provider {
    @Inject
    lateinit var images: Lazy<ImageLoader>

    @Inject
    lateinit var workerFactory: HiltWorkerFactory

    /** Injected so the signed-in account's store opens at start. */
    @Inject
    lateinit var accountStores: AccountStores

    /** Injected so the outbox and the pending rows left by the last run are sent at start. */
    @Inject
    lateinit var readingSync: ReadingSyncManager

    @Inject
    lateinit var syncCenter: SyncCenter

    @Inject
    lateinit var downloads: DownloadRepository

    @Inject
    lateinit var refresher: CatalogRefresher

    @Inject
    lateinit var lists: ListsRepository

    @Inject
    lateinit var connectivity: NetworkConnectivity

    @Inject
    lateinit var syncWork: SyncWork

    @Inject
    lateinit var events: CatalogEvents

    @Inject
    lateinit var session: SessionStore

    @Inject
    @AppScope
    lateinit var scope: CoroutineScope

    override fun onCreate() {
        // Before super.onCreate, so the injection of the singletons below is covered too.
        if (applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE != 0) {
            StrictMode.setThreadPolicy(StrictMode.ThreadPolicy.Builder().detectDiskReads().detectDiskWrites().detectNetwork().penaltyLog().build())
        }
        super.onCreate()
        // After injection: WorkManager needs the worker factory.
        downloads.begin()
        syncWork.begin()
        refresher.begin()
        connectivity.begin()
        // Each screen on a back stack reloads once when the server comes or goes (P2 §10).
        scope.launch { connectivity.online.drop(1).collect { events.emit(CatalogChange.OnlineChanged(it)) } }
        // Covers and pages are cached per server, not per user: every end of a signed-in session (a 401 too, not only the sign-out button) clears them.
        scope.launch {
            var signedIn = false
            session.state.collect { state ->
                val now = state is SessionState.SignedIn
                if (signedIn && !now) {
                    withContext(Dispatchers.IO) {
                        runCatching {
                            images.get().memoryCache?.clear()
                            images.get().diskCache?.clear()
                        }
                    }
                }
                signedIn = now
            }
        }
        ProcessLifecycleOwner.get().lifecycle.addObserver(
            LifecycleEventObserver { _, event ->
                if (event == Lifecycle.Event.ON_START) {
                    connectivity.foreground(true)
                    // An account whose database couldn't be opened gets another try (the disk may have room).
                    scope.launch { accountStores.retry() }
                    syncCenter.foreground()
                    refresher.refresh(automatic = true)
                    lists.foreground()
                } else if (event == Lifecycle.Event.ON_STOP) {
                    connectivity.foreground(false)
                    readingSync.background()
                    syncCenter.background()
                }
            },
        )
    }

    /** Composables use the one loader without it being passed down. */
    override fun newImageLoader(context: PlatformContext): ImageLoader = images.get()

    /** Workers get Hilt's singletons; the manifest removes WorkManager's own initializer. */
    override val workManagerConfiguration: Configuration
        get() = Configuration.Builder().setWorkerFactory(workerFactory).build()
}
