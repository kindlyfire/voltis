package me.tijlvdb.voltis.data.db

import android.content.Context
import android.content.pm.ApplicationInfo
import android.util.Log
import androidx.room.Room
import androidx.sqlite.db.SupportSQLiteDatabase
import androidx.sqlite.db.SupportSQLiteOpenHelper
import androidx.sqlite.db.framework.FrameworkSQLiteOpenHelperFactory
import coil3.ImageLoader
import dagger.Lazy
import dagger.hilt.android.qualifiers.ApplicationContext
import java.io.File
import java.security.MessageDigest
import java.util.concurrent.CopyOnWriteArrayList
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import me.tijlvdb.voltis.data.auth.AppScope
import me.tijlvdb.voltis.data.auth.SessionStore

/** Something that uses the open account's store and must stop before it closes. */
interface AccountScoped {
    /**
     * Cancels and joins everything this component started, even when its own cleanup fails
     * (`try { cleanup() } finally { scope.cancel(); job.join() }`): the database closes right after.
     */
    suspend fun stop()
}

/** An account whose data a sign-out asked to delete (stored with the sign-out), until its deletion is done. */
interface DeleteIntents {
    suspend fun pending(): String?

    suspend fun clear(account: String)

    /** Whether [account]'s intent is still stored, read from the published state without waiting: for a check inside the session pin. */
    fun isPending(account: String): Boolean

    object None : DeleteIntents {
        override suspend fun pending(): String? = null

        override suspend fun clear(account: String) = Unit

        override fun isPending(account: String) = false
    }
}

/** The open account's store. Compared by identity: each sign-in opens a new one. */
interface OpenAccount {
    val account: String
}

/** An account's open database, in its own directory. [account] is `SessionState.account`. */
class AccountStore(override val account: String, val dir: File, val db: VoltisDatabase) : OpenAccount

/** An account's directory other than the open one's: [account] from its `account` file, [bytes] everything in it. */
data class StoredAccount(val dir: File, val account: String?, val bytes: Long)

/** What an account left on this device: its download rows and its unsent changes (reading ops, and stars and ratings). */
data class AccountData(val downloads: Int, val unsent: Int) {
    val any get() = downloads > 0 || unsent > 0
}

/**
 * One store per account, under `noBackupFilesDir/accounts/<hash>/`: open while the session is
 * signed in to that account, closed otherwise and kept on disk. Repositories read through [current],
 * never holding a DAO across a change.
 *
 * Only two things delete an account's directory (P2 §3, §11): sign-out with "Also delete them", and
 * "Data from other accounts". Under [lock] the directory is checked against the live session and the
 * open store, then moved into [quarantine] (the durable intent); there the rows go (one transaction),
 * the database closes, and the files go, `voltis.db` first. The start sweeps whatever is quarantined,
 * and the directories without a `voltis.db` that older builds left.
 */
@Singleton
class AccountStores internal constructor(
    private val context: Context,
    /** `accounts/`; asked for off the main thread: `noBackupFilesDir` stats the disk. */
    rootDir: () -> File,
    /** The signed-in account, `SessionState.account`. */
    accounts: Flow<String?>,
    /** The signed-in account right now, which [accounts] reaches only after a delay: deletions check this. */
    private val liveAccount: () -> String?,
    scope: CoroutineScope,
    /** Debug builds recreate a database they can't migrate or downgrade; a release build never does: it would lose the outbox. Off in tests. */
    private val destructiveFallback: Boolean = context.applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE != 0,
    /** The image cache is the one large thing the app can give back by itself when a database won't open. */
    private val clearImageCache: () -> Unit = {},
    private val intents: DeleteIntents = DeleteIntents.None,
    /**
     * Runs a step while no session change can be published (`SessionStore.pinned`). Admitting a deletion
     * is checked against the live session inside it, so a sign-in can't land between check and rename.
     */
    private val pin: suspend (() -> Unit) -> Unit = { it() },
) {
    @Inject constructor(@ApplicationContext context: Context, session: SessionStore, @AppScope scope: CoroutineScope, images: Lazy<ImageLoader>) :
        this(
            context, { File(context.noBackupFilesDir, "accounts") }, session.state.map { it.account }, { session.state.value.account }, scope,
            clearImageCache = { images.get().diskCache?.clear() },
            intents = session, pin = session::pinned,
        )

    /** Directories being deleted, moved here under [lock] first: a kill leaves the intent, and [sweep] finishes it. */
    private val root by lazy(rootDir)
    private val quarantine by lazy { File(root.parentFile, "${root.name}-deleting") }

    private val _current = MutableStateFlow<AccountStore?>(null)
    val current: StateFlow<AccountStore?> = _current.asStateFlow()
    private val _failed = MutableStateFlow<String?>(null)

    /** The signed-in account whose database couldn't be opened ("Couldn't open offline data"). [current] is null then too. */
    val failed: StateFlow<String?> = _failed.asStateFlow()
    private val scoped = CopyOnWriteArrayList<AccountScoped>()

    /** The account the last switch went to, open or failed; null signed out or before the first switch. */
    private val switched = MutableStateFlow<String?>(null)

    /** Held by a switch, and by whatever opens or deletes a closed account's database: one at a time. */
    private val lock = Mutex()

    init {
        // Sequential: a change finishes closing before the next store opens, and the sweep comes first.
        scope.launch {
            // A failing sweep is logged; opening the account still happens.
            try {
                lock.withLock { withContext(Dispatchers.IO) { sweep() } }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Log.e(TAG, "Couldn't sweep the accounts directory", e)
            }
            // A sign-out's "Also delete them" that a kill cut short.
            try {
                // Valid only while the intent stands: a sign-in since it was read cleared it.
                intents.pending()?.let { if (deleteClosed(it) { intents.isPending(it) }) intents.clear(it) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Log.e(TAG, "Couldn't finish a stored deletion", e)
            }
            accounts.distinctUntilChanged().collect { lock.withLock { switchTo(it) } }
        }
    }

    /** [component] is stopped, and waited for, before every close. */
    fun register(component: AccountScoped) {
        scoped += component
    }

    /**
     * The order components rely on: `current` goes null first, then every component's `stop()` is
     * called and joined, then the database closes, and only then the next store opens. A store
     * instance is therefore a generation: `current !== store` means everything of that store's
     * account must stop.
     */
    private suspend fun switchTo(account: String?) {
        _current.value?.let { old ->
            _current.value = null
            try {
                // Every component gets its stop() before the close, even if this switch is cancelled.
                withContext(NonCancellable) {
                    for (component in scoped) {
                        try {
                            component.stop()
                        } catch (e: Exception) {
                            Log.e(TAG, "Couldn't stop $component", e)
                        }
                    }
                }
            } finally {
                runCatching { old.db.close() }.onFailure { Log.e(TAG, "Couldn't close ${old.dir.name}", it) }
            }
        }
        _failed.value = null
        switched.value = account
        if (account != null) {
            val store = open(account)
            _current.value = store
            if (store == null) _failed.value = account
        }
    }

    /**
     * Opens the signed-in account's store again after [failed] (a full disk that has since been
     * freed, say). Nothing when the session has moved on: the account's own switch handles that.
     */
    suspend fun retry() = lock.withLock {
        val account = failed.value ?: return@withLock
        if (liveAccount() == account && switched.value == account) switchTo(account)
    }

    /** Null when the database can't be opened: nothing is deleted for it. */
    private suspend fun open(account: String): AccountStore? = withContext(Dispatchers.IO) {
        // Resolving the root is part of opening: when it throws, the account fails like a database that won't open.
        val dir = try {
            dirOf(account)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Log.e(TAG, "Couldn't resolve the accounts directory", e)
            return@withContext null
        }
        tryOpen(account, dir) ?: run {
            // Often a full disk: the cache goes, then one more try.
            try {
                clearImageCache()
            } catch (e: Exception) {
                Log.w(TAG, "Couldn't clear the image cache", e)
            }
            tryOpen(account, dir)
        }
    }

    private fun tryOpen(account: String, dir: File): AccountStore? {
        var db: VoltisDatabase? = null
        return try {
            dir.mkdirs()
            File(dir, "account").takeUnless { it.exists() }?.writeText(account)
            db = build(dir)
            db.openHelper.writableDatabase
            AccountStore(account, dir, db)
        } catch (e: Exception) {
            Log.e(TAG, "Couldn't open ${dir.name}", e)
            db?.close()
            null
        }
    }

    private fun build(dir: File): VoltisDatabase {
        val builder = Room.databaseBuilder(context, VoltisDatabase::class.java, File(dir, DB).path).openHelperFactory(KeepCorruptFactory())
        if (destructiveFallback) builder.fallbackToDestructiveMigration(dropAllTables = true)
        return builder.build()
    }

    private fun dirOf(account: String) = File(root, directoryName(account))

    /** At start, before anything opens: quarantined deletions, and directories without `voltis.db`. */
    private fun sweep() {
        for (dir in quarantine.listFiles().orEmpty()) if (dir.isDirectory) finish(dir) else dir.delete()
        for (dir in root.listFiles().orEmpty()) {
            if (dir.isDirectory && !File(dir, DB).exists() && !dir.deleteRecursively()) Log.e(TAG, "Couldn't sweep ${dir.name}")
        }
    }

    /** Every account directory but [except]'s (the signed-in account, open or not). */
    suspend fun others(except: String?): List<StoredAccount> = lock.withLock {
        withContext(Dispatchers.IO) {
            val skip = except?.let(::directoryName)
            root.listFiles().orEmpty().filter { it.isDirectory && it.name != skip }.map { dir ->
                StoredAccount(dir, File(dir, "account").takeIf { it.exists() }?.readText(), dir.walkBottomUp().filter { it.isFile }.sumOf { it.length() })
            }
        }
    }

    /**
     * What the closed accounts of [serverId] left on this device: the Change-server text (P2 §11).
     * Their databases are opened one at a time to count; one that can't be opened counts as having data.
     */
    suspend fun dataOf(serverId: String): AccountData = lock.withLock {
        withContext(Dispatchers.IO) {
            var total = AccountData(0, 0)
            for (dir in root.listFiles().orEmpty()) {
                val account = File(dir, "account").takeIf { it.isFile }?.readText() ?: continue
                if (!account.startsWith("$serverId/") || account == switched.value || account == liveAccount() || !File(dir, DB).exists()) continue
                val data = try {
                    val db = build(dir)
                    try {
                        val sql = db.openHelper.writableDatabase
                        AccountData(sql.count("download"), sql.count("reading_op") + sql.count("pending_user_data WHERE wanted = 1"))
                    } finally {
                        db.close()
                    }
                } catch (e: Exception) {
                    Log.e(TAG, "Couldn't read ${dir.name}", e)
                    AccountData(1, 0)
                }
                total = AccountData(total.downloads + data.downloads, total.unsent + data.unsent)
            }
            total
        }
    }

    /**
     * Deletes [account]'s data once its store has closed (a sign-out that just happened): rows, close,
     * directory. Nothing when it doesn't close within a few seconds, or [valid] no longer holds (a
     * newer sign-in), or the account is signed in again, whether or not its store has opened yet.
     * True when it is gone (renamed away, or never there), false when it was left: not allowed, or the rename failed.
     */
    suspend fun deleteClosed(account: String, valid: () -> Boolean = { true }): Boolean {
        // first() returns the account switched to, null when signed out: the result is a Boolean.
        withTimeoutOrNull(CLOSE_WAIT) { switched.first { it != account }.let { true } } ?: return false
        return lock.withLock {
            val admitted = withContext(Dispatchers.IO) {
                admit(dirOf(account)) { valid() && liveAccount() != account && switched.value != account }
            }
            admitted.doomed?.let { withContext(Dispatchers.IO) { purge(it) } }
            admitted.settled
        }
    }

    /** "Data from other accounts": every directory [others] lists for [except], never the live session's or the open store's. */
    suspend fun deleteOthers(except: String?) = lock.withLock {
        for (dir in withContext(Dispatchers.IO) { root.listFiles().orEmpty() }) {
            val doomed = withContext(Dispatchers.IO) {
                admit(dir) {
                    // Read per directory, inside the pin: a sign-in can land while an earlier one is being deleted.
                    val keep = setOfNotNull(except, liveAccount(), switched.value).map(::directoryName)
                    dir.isDirectory && dir.name !in keep
                }
            }
            doomed.doomed?.let { withContext(Dispatchers.IO) { purge(it) } }
        }
    }

    /**
     * Must hold [lock], with [dir]'s database closed. Moves [dir] into the quarantine (the durable
     * intent) while [allowed] still holds, checked inside the [pin]: a sign-in is either before it,
     * and keeps the directory, or after the rename, and finds it empty.
     */
    private suspend fun admit(dir: File, allowed: () -> Boolean): Admitted {
        if (!dir.exists()) return Admitted(null, settled = true)
        quarantine.mkdirs()
        val doomed = File(quarantine, dir.name)
        // A leftover of the same name: an earlier delete that couldn't finish. Before the pin: it can be slow.
        if (doomed.exists()) finish(doomed)
        var admitted = Admitted(null, settled = false)
        pin {
            if (allowed()) {
                if (dir.renameTo(doomed)) admitted = Admitted(doomed, settled = true) else Log.e(TAG, "Couldn't quarantine ${dir.name}")
            }
        }
        return admitted
    }

    /** [doomed] is the quarantined directory to purge. [settled]: it is moved, or was never there; false when left (not allowed, or the rename failed). */
    private class Admitted(val doomed: File?, val settled: Boolean)

    /** A quarantined directory: the rows first. A database that can't be opened is deleted all the same: the user asked. */
    private fun purge(doomed: File) {
        if (File(doomed, DB).exists()) {
            try {
                val db = build(doomed)
                try {
                    db.clearAllTables()
                } finally {
                    db.close()
                }
            } catch (e: Exception) {
                Log.e(TAG, "Couldn't clear ${doomed.name}", e)
            }
        }
        finish(doomed)
    }

    /** Removes a quarantined [dir]: the database files first. */
    private fun finish(dir: File) {
        for (name in listOf(DB, "$DB-wal", "$DB-shm", "$DB-journal")) File(dir, name).delete()
        if (!dir.deleteRecursively()) Log.e(TAG, "Couldn't delete all of ${dir.name}")
    }

    /**
     * Android's default reaction to a corrupt database is deleting its file. Here the open fails and
     * the file stays: nothing is deleted for a database that won't open (P2 §3).
     */
    private class KeepCorruptFactory : SupportSQLiteOpenHelper.Factory {
        override fun create(configuration: SupportSQLiteOpenHelper.Configuration): SupportSQLiteOpenHelper {
            val inner = configuration.callback
            val keeping = object : SupportSQLiteOpenHelper.Callback(inner.version) {
                override fun onConfigure(db: SupportSQLiteDatabase) = inner.onConfigure(db)

                override fun onCreate(db: SupportSQLiteDatabase) = inner.onCreate(db)

                override fun onUpgrade(db: SupportSQLiteDatabase, oldVersion: Int, newVersion: Int) = inner.onUpgrade(db, oldVersion, newVersion)

                override fun onDowngrade(db: SupportSQLiteDatabase, oldVersion: Int, newVersion: Int) = inner.onDowngrade(db, oldVersion, newVersion)

                override fun onOpen(db: SupportSQLiteDatabase) = inner.onOpen(db)

                override fun onCorruption(db: SupportSQLiteDatabase) {
                    Log.e(TAG, "Corruption reported by sqlite in ${db.path}; the file is kept")
                }
            }
            return FrameworkSQLiteOpenHelperFactory().create(
                SupportSQLiteOpenHelper.Configuration.builder(configuration.context)
                    .name(configuration.name)
                    .callback(keeping)
                    .noBackupDirectory(configuration.useNoBackupDirectory)
                    .allowDataLossOnRecovery(configuration.allowDataLossOnRecovery)
                    .build(),
            )
        }
    }

    companion object {
        private const val TAG = "AccountStores"
        private const val DB = "voltis.db"

        /** How long a delete after sign-out waits for the account's store to close. */
        private const val CLOSE_WAIT = 10_000L

        /** The first 16 hex characters of the account's SHA-256. */
        fun directoryName(account: String): String =
            MessageDigest.getInstance("SHA-256").digest(account.toByteArray()).take(8).joinToString("") { "%02x".format(it) }

        private fun SupportSQLiteDatabase.count(table: String) = query("SELECT COUNT(*) FROM $table").use { if (it.moveToFirst()) it.getInt(0) else 0 }
    }
}
