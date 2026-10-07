package me.tijlvdb.voltis.ui.settings

import me.tijlvdb.voltis.data.downloads.DownloadSnapshot
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

class DownloadSettingsContentTest {
    private val snapshot = DownloadSnapshot("a", emptyList(), 0, emptySet(), 100)

    @Test
    fun theOtherAccountsRowIsReachableWhenTheStoreFailed() {
        // Nothing before the other accounts are read, however the store went.
        assertNull(settingsContent(null, "a", "a", true, null, othersRead = false))
        // A failed store is final: ready with the error and the other accounts' bytes.
        val failed = settingsContent(null, "a", "a", true, 500, othersRead = true)!!
        assertNull(failed.stored)
        assertEquals(500L, failed.others)
        // A healthy one waits for its own snapshot, and refuses another account's.
        assertNull(settingsContent(null, null, "a", true, null, othersRead = true))
        assertNull(settingsContent(snapshot, null, "b", true, null, othersRead = true))
        assertNotNull(settingsContent(snapshot, null, "a", true, null, othersRead = true)!!.stored)
        // Another account's failure is no state of this page: unknown without a matching snapshot, and a matching one wins.
        assertNull(settingsContent(null, "b", "a", true, 500, othersRead = true))
        assertNotNull(settingsContent(snapshot, "b", "a", true, 500, othersRead = true)!!.stored)
    }
}
