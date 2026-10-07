package me.tijlvdb.voltis.data.reading

import org.junit.Assert.assertEquals
import org.junit.Test

/** readingSync.test.ts, `hide`: the web's `hide()` is `setVisible(false)`, and its keepalive flag has no counterpart. */
class EngineHideTest : EngineTest() {
    @Test
    fun `sends unsent reading with keepalive when nothing is out`() = engineTest {
        val (sync) = open("c")
        sync.moved(page(3))
        sync.setVisible(false)
        settle()
        assertEquals(page(3), server.sent.last().body?.progress())
    }

    @Test
    fun `leaves reading for later when a request is out, and loses nothing if the page lives`() = engineTest {
        val (sync) = open("c")
        server.holding = true
        read(sync, 1)
        sync.moved(page(2))
        sync.setVisible(false)
        settle()
        assertEquals(1, writes().size)
        server.release()
        settle()
        assertEquals(page(2), server.stateOf("c").progress)
    }

    @Test
    fun `retries a failed keepalive on Retry`() = engineTest {
        val (sync) = open("c")
        server.failing = true
        sync.moved(page(3))
        sync.setVisible(false)
        settle()
        server.failing = false
        sync.retry()
        settle()
        assertEquals(page(3), server.stateOf("c").progress)
    }
}
