package me.tijlvdb.voltis.data.db

import android.app.Application
import androidx.test.core.app.ApplicationProvider
import java.io.File
import java.util.concurrent.CopyOnWriteArrayList
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [37], application = Application::class)
class AccountStoresTest {
    @get:Rule
    val temp = TemporaryFolder()

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    @After
    fun stop() = scope.cancel()

    private suspend fun AccountStores.opened(account: String) = withTimeout(10_000) { current.first { it?.account == account } }!!

    @Test
    fun accountsKeepTheirDirectoriesUntilDeleted() = runBlocking {
        val root = temp.newFolder("accounts")
        // What a delete cut short by a kill leaves: no voltis.db, so it is swept at start.
        val stray = File(root, "0123456789abcdef/downloads").apply { mkdirs() }.parentFile!!
        File(stray, "downloads/page").writeText("x")
        // A kill after the rows were cleared, before the database was unlinked: quarantined, so swept.
        val halfDeleted = File(temp.root, "accounts-deleting/fedcba9876543210").apply { mkdirs() }
        File(halfDeleted, "voltis.db").writeText("")
        File(halfDeleted, "downloads").apply { mkdirs() }.resolve("page").writeText("x")
        // `account` is what the session is signed in as; the stores follow it through `delivered`, a moment later.
        val account = MutableStateFlow<String?>("srv/a")
        val delivered = MutableStateFlow<String?>("srv/a")
        val stores = AccountStores(ApplicationProvider.getApplicationContext(), { root }, delivered, { account.value }, scope)
        lateinit var a: AccountStore
        var openAtStop: Boolean? = null
        stores.register(
            object : AccountScoped {
                override suspend fun stop() {
                    // No longer current, and not closed yet.
                    openAtStop = stores.current.value == null && a.db.isOpen
                    // A stop() that throws is logged; the close goes on.
                    error("stop failed")
                }
            },
        )

        a = stores.opened("srv/a")
        assertFalse(stray.exists())
        assertFalse(halfDeleted.exists())
        a.db.ops().insert(listOf(op("c_1")))
        // A star with a wish counts as unsent; one only kept for its landed values doesn't.
        fun star(id: String, wanted: Boolean) = PendingUserDataEntity(id, null, null, id, wanted, true, false, null, 1, 1, true, null, 0, null, 0)
        a.db.pendingUserData().upsert(star("c_2", wanted = true))
        a.db.pendingUserData().upsert(star("c_3", wanted = false))
        File(a.dir, "downloads/copy").apply { mkdirs() }.resolve("0").writeText("page")

        account.value = "srv/b"
        delivered.value = "srv/b"
        val b = stores.opened("srv/b")
        assertEquals(true, openAtStop)
        assertFalse(a.db.isOpen)
        assertTrue(b.dir != a.dir)
        // The first account's directory stays as it was, and is listed as another account's.
        assertTrue(File(a.dir, "voltis.db").exists() && File(a.dir, "downloads/copy/0").exists())
        assertEquals(listOf("srv/a"), stores.others(except = "srv/b").map { it.account })
        assertEquals(AccountData(downloads = 0, unsent = 2), stores.dataOf("srv"))

        // Sign-out with "Also delete them": once the store has closed, rows, then the directory.
        account.value = null
        delivered.value = null
        stores.deleteClosed("srv/b")
        assertNull(stores.current.value)
        assertFalse(b.dir.exists())
        assertTrue(a.dir.exists())

        // Signed in to the first account again, but its store not opened yet: nothing of it is deleted.
        account.value = "srv/a"
        stores.deleteClosed("srv/a")
        stores.deleteOthers(except = null)
        assertTrue(File(a.dir, "voltis.db").exists() && File(a.dir, "downloads/copy/0").exists())
        // A sign-out that a newer sign-in has overtaken is dropped too.
        account.value = null
        stores.deleteClosed("srv/a") { false }
        assertTrue(File(a.dir, "downloads/copy/0").exists())

        // "Data from other accounts".
        stores.deleteOthers(except = null)
        assertFalse(a.dir.exists())
        assertEquals(emptyList<StoredAccount>(), stores.others(except = null))

        // Signing in again starts over, empty.
        account.value = "srv/a"
        delivered.value = "srv/a"
        assertEquals(0, stores.opened("srv/a").db.ops().count().first())
    }

    /** A database that can't be opened (a version above the app's, or no database at all) is left as it is: the app says so, and nothing is deleted. */
    @Test
    fun aDatabaseThatWontOpenIsKeptAndTheImageCacheIsClearedOnce() = runBlocking {
        val root = temp.newFolder("accounts")
        fun planted(account: String) = File(root, "${AccountStores.directoryName(account)}/voltis.db").apply { parentFile!!.mkdirs() }
        val tooNew = planted("srv/new")
        android.database.sqlite.SQLiteDatabase.openOrCreateDatabase(tooNew, null).use {
            it.execSQL("CREATE TABLE kept (x TEXT)")
            it.execSQL("INSERT INTO kept VALUES ('rows')")
            it.version = 99
        }
        val garbage = planted("srv/garbage").also { it.writeBytes(ByteArray(4096) { i -> (i * 7).toByte() }) }
        val garbageBytes = garbage.readBytes()
        val account = MutableStateFlow<String?>("srv/new")
        var cleared = 0
        val stores = AccountStores(
            ApplicationProvider.getApplicationContext(), { root }, account, { account.value }, scope,
            destructiveFallback = false, clearImageCache = { cleared++ },
        )
        withTimeout(10_000) { stores.failed.first { it == "srv/new" } }
        assertNull(stores.current.value)
        // Cleared once, and tried once more.
        assertEquals(1, cleared)
        android.database.sqlite.SQLiteDatabase.openDatabase(tooNew.path, null, android.database.sqlite.SQLiteDatabase.OPEN_READONLY).use {
            assertEquals(99, it.version)
            assertEquals("rows", it.rawQuery("SELECT x FROM kept", null).use { c -> c.moveToFirst(); c.getString(0) })
        }

        account.value = "srv/garbage"
        withTimeout(10_000) { stores.failed.first { it == "srv/garbage" } }
        assertEquals(2, cleared)
        assertTrue(garbage.exists())
        assertTrue(garbageBytes.contentEquals(garbage.readBytes()))
    }

    /** A root that can't be resolved fails the account like a database that won't open, and later switches still happen. */
    @Test
    fun anUnresolvableRootFailsTheAccountAndKeepsCollecting() = runBlocking {
        val account = MutableStateFlow<String?>("srv/a")
        val accounts = temp.newFolder("accounts")
        var broken = true
        val stores = AccountStores(
            ApplicationProvider.getApplicationContext(), { if (broken) error("no disk") else accounts }, account, { account.value }, scope,
            destructiveFallback = false,
        )
        withTimeout(10_000) { stores.failed.first { it == "srv/a" } }
        assertNull(stores.current.value)


        account.value = "srv/b"
        withTimeout(10_000) { stores.failed.first { it == "srv/b" } }

        // The cause went away, the session didn't change: a retry opens the account's store.
        broken = false
        stores.retry()
        assertEquals("srv/b", stores.current.value?.account)
        assertNull(stores.failed.value)
        account.value = null
        withTimeout(10_000) { stores.failed.first { it == null } }
        Unit
    }

    /**
     * A sign-in landing after a deletion's checks must still keep the data: the checks run inside the session pin,
     * for both entry points. A deletion a kill cut short (its stored intent) is finished at the start.
     */
    @Test
    fun aSignInBeforeTheRenameKeepsTheDataAndAStoredIntentIsFinished() = runBlocking {
        val root = temp.newFolder("accounts")
        fun planted(account: String) = File(root, AccountStores.directoryName(account)).also { File(it, "voltis.db").apply { parentFile!!.mkdirs() }.writeText("x") }
        val a = planted("srv/a")
        val b = planted("srv/b")
        val live = MutableStateFlow<String?>(null)
        var signIn: String? = null
        val stores = AccountStores(
            ApplicationProvider.getApplicationContext(), { root }, MutableStateFlow(null), { live.value }, scope,
            destructiveFallback = false, pin = { step -> signIn?.let { live.value = it }; step() },
        )
        signIn = "srv/a"
        assertFalse(stores.deleteClosed("srv/a"))
        assertTrue(a.exists())
        signIn = "srv/b"
        stores.deleteOthers(except = null)
        assertTrue(b.exists())
        assertFalse(a.exists())

        planted("srv/a")
        val cleared = CopyOnWriteArrayList<String>()
        val clearedOnce = kotlinx.coroutines.CompletableDeferred<Unit>()
        val intent = object : DeleteIntents {
            override suspend fun pending() = "srv/a"

            override suspend fun clear(account: String) {
                cleared += account
                clearedOnce.complete(Unit)
            }

            override fun isPending(account: String) = true
        }
        AccountStores(
            ApplicationProvider.getApplicationContext(), { root }, MutableStateFlow(null), { null }, scope,
            destructiveFallback = false, intents = intent,
        )
        withTimeout(10_000) { clearedOnce.await() }
        assertEquals(listOf("srv/a"), cleared)
        assertFalse(a.exists())

        // A replay that read its intent, then lost it to a sign-in whose session has since ended by a 401: the data stays.
        val resume = kotlinx.coroutines.CompletableDeferred<Unit>()
        var standing = true
        val cleared2 = CopyOnWriteArrayList<String>()
        val survivor = planted("srv/a")
        AccountStores(
            ApplicationProvider.getApplicationContext(), { root }, MutableStateFlow(null), { null }, scope, destructiveFallback = false,
            intents = object : DeleteIntents {
                override suspend fun pending(): String {
                    resume.await()
                    return "srv/a"
                }

                override suspend fun clear(account: String) {
                    cleared2 += account
                }

                override fun isPending(account: String) = standing
            },
        )
        standing = false
        resume.complete(Unit)
        kotlinx.coroutines.delay(500)
        assertTrue(survivor.exists())
        assertEquals(emptyList<String>(), cleared2)

        // Nothing there: settled. A rename that fails (no quarantine can be made) leaves the data, and the deletion open.
        val plain = AccountStores(ApplicationProvider.getApplicationContext(), { root }, MutableStateFlow(null), { null }, scope, destructiveFallback = false)
        assertTrue(plain.deleteClosed("srv/none"))
        val kept = planted("srv/kept")
        File(temp.root, "accounts-deleting").apply { deleteRecursively() }.writeText("in the way")
        assertFalse(plain.deleteClosed("srv/kept"))
        assertTrue(kept.exists())
    }

    private fun op(contentId: String) = OpEntity(0, contentId, "position", "{}", true, null, null, null, null, null, 0, null, 0)
}
