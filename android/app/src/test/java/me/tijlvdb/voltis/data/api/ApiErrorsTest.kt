package me.tijlvdb.voltis.data.api

import java.io.IOException
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertSame
import org.junit.Test

class ApiErrorsTest {
    @Test
    fun cancellationPropagatesAndFailuresAreReturned() = runTest {
        // Thrown into a caller that is still active, as by a nested timeout: not a failure to show.
        val cancelled = CancellationException("superseded")
        assertSame(cancelled, runCatching { attemptResult<Unit> { throw cancelled } }.exceptionOrNull())

        val failure = IOException("offline")
        assertSame(failure, attemptResult<Unit> { throw failure }.exceptionOrNull())
    }
}
