package me.tijlvdb.voltis.ui.nav

import android.content.Intent
import org.junit.Assert.assertEquals
import org.junit.Test

class LaunchTargetTest {
    @Test
    fun parsesActions() {
        assertEquals(LaunchTarget.Search, launchTargetOf(LaunchTarget.ACTION_SEARCH, 0))
        assertEquals(LaunchTarget.Downloads, launchTargetOf(LaunchTarget.ACTION_DOWNLOADS, Intent.FLAG_ACTIVITY_NEW_TASK))
        assertEquals(LaunchTarget.Continue, launchTargetOf(LaunchTarget.ACTION_CONTINUE, 0))
        assertEquals(null, launchTargetOf(Intent.ACTION_MAIN, 0))
        assertEquals(null, launchTargetOf(null, 0))
        // Recents replays the intent that started the task.
        assertEquals(null, launchTargetOf(LaunchTarget.ACTION_SEARCH, Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY))
    }
}
