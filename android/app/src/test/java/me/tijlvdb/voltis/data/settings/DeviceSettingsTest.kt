package me.tijlvdb.voltis.data.settings

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import java.io.File
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import me.tijlvdb.voltis.domain.catalog.GridOptions
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class DeviceSettingsTest {
    @get:Rule
    val tmp = TemporaryFolder()

    @Test(timeout = 20_000)
    fun gridOptionsAreOneSetKeepingTheOldDefaultGroup() = runBlocking {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        val store = PreferenceDataStoreFactory.create(scope = scope) { File(tmp.root, "device.preferences_pb") }
        val default = stringPreferencesKey("grid_options:default")
        val browse = stringPreferencesKey("grid_options:browse")
        val facet = stringPreferencesKey("grid_options:facet")
        store.edit {
            it[default] = """{"itemSize": 200, "hideTitle": true}"""
            it[browse] = """{"itemSize": 300}"""
            it[facet] = """{"itemSize": 250}"""
        }
        val settings = DeviceSettings(store)
        // What the default group had is what every grid shows now.
        assertEquals(GridOptions(itemSize = 200, hideTitle = true), settings.gridOptions.first())

        settings.updateGridOptions { it.copy(itemSize = 150) }
        assertEquals(GridOptions(itemSize = 150, hideTitle = true), settings.gridOptions.first())
        val prefs = store.data.first()
        assertNull(prefs[browse])
        assertNull(prefs[facet])

        settings.resetGridOptions()
        assertEquals(GridOptions(), settings.gridOptions.first())
        scope.cancel()
    }
}
