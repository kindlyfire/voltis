package me.tijlvdb.voltis.data.storage

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test

/** Plain JVM: the clock is injected, and [StorageAlerts] is global, so it is reset around the test. */
class StorageAlertsTest {
    @Before
    @After
    fun reset() = StorageAlerts.reset()

    @OptIn(ExperimentalCoroutinesApi::class)
    @Test
    fun aDiskThatStaysFullGivesOneAlert() = runBlocking {
        var now = 1_000L
        StorageAlerts.clock = { now }
        val alerts = mutableListOf<Unit>()
        val job = launch(Dispatchers.Unconfined) { StorageAlerts.full.collect { alerts += it } }
        StorageAlerts.full()
        now += 10_000
        StorageAlerts.full()
        assertEquals(1, alerts.size)
        // Measured from the last call: it kept failing, so it stays quiet; a quiet half minute is another alert.
        now += 29_000
        StorageAlerts.full()
        assertEquals(1, alerts.size)
        now += 40_000
        StorageAlerts.full()
        assertEquals(2, alerts.size)
        job.cancel()
    }
}
