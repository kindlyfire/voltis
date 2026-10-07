package me.tijlvdb.voltis.ui.reader

import android.app.Application
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import androidx.lifecycle.viewModelScope
import java.io.IOException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import me.tijlvdb.voltis.data.settings.DeviceSettings
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [37], application = Application::class)
class TutorialViewModelTest {
    @Before
    fun setUp() = Dispatchers.setMain(UnconfinedTestDispatcher())

    @After
    fun tearDown() = Dispatchers.resetMain()

    /** A device whose settings can't be written, for a reason that isn't a full disk either. */
    private val unwritable = object : DataStore<Preferences> {
        override val data = flowOf(emptyPreferences())

        override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences = throw IOException("read-only")
    }

    @Test
    fun dismissingNeedsNoStorage() {
        val vm = TutorialViewModel(DeviceSettings(unwritable))
        assertEquals(false, vm.seen.value)
        // Done, Back and a tap outside all end here: the flag can't be stored, and the tutorial still closes, without a crash.
        vm.dismiss()
        assertEquals(true, vm.seen.value)
        vm.viewModelScope.cancel()
    }
}
