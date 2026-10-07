package me.tijlvdb.voltis.ui.nav

import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.SnackbarVisuals
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class PresentWhileTest {
    private val visuals = object : SnackbarVisuals {
        override val message = "Marked read"
        override val actionLabel: String? = null
        override val withDismissAction = false
        override val duration = SnackbarDuration.Indefinite
    }

    /** A summary shows only while it is valid: never behind an Undo that outlives it, and not on past it. */
    @Test
    fun cancelsWhenInvalid() = runTest {
        // Waiting behind an Undo.
        var host = SnackbarHostState()
        var valid = MutableStateFlow(true)
        launch { host.showSnackbar("Undo") }
        runCurrent()
        var result = async { host.presentWhile(visuals, valid) }
        runCurrent()
        valid.value = false
        runCurrent()
        host.currentSnackbarData?.dismiss()
        runCurrent()
        assertNull(result.await())
        assertNull(host.currentSnackbarData)

        // Visible.
        host = SnackbarHostState()
        valid = MutableStateFlow(true)
        result = async { host.presentWhile(visuals, valid) }
        runCurrent()
        assertSame(visuals, host.currentSnackbarData?.visuals)
        valid.value = false
        runCurrent()
        assertNull(result.await())
        assertNull(host.currentSnackbarData)

        // Dismissed normally.
        host = SnackbarHostState()
        valid = MutableStateFlow(true)
        result = async { host.presentWhile(visuals, valid) }
        runCurrent()
        host.currentSnackbarData?.dismiss()
        runCurrent()
        assertEquals(SnackbarResult.Dismissed, result.await())
    }
}
