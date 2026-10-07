package me.tijlvdb.voltis.data.settings

import android.util.Log
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.MutablePreferences
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import java.io.IOException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.serialization.SerializationException
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.storage.localWrite
import me.tijlvdb.voltis.domain.catalog.GridOptions
import me.tijlvdb.voltis.domain.storage.StorageFullException

enum class ThemeMode { System, Light, Dark }

/** Settings of this install, not of a server or a user. */
@Singleton
class DeviceSettings @Inject constructor(@param:DeviceStore private val store: DataStore<Preferences>) {
    val theme: Flow<ThemeMode> = store.data
        .map { prefs -> ThemeMode.entries.find { it.name == prefs[THEME] } ?: ThemeMode.System }
        .distinctUntilChanged()

    suspend fun setTheme(mode: ThemeMode) {
        edit { it[THEME] = mode.name }
    }

    /** Per device: the server's `tutorials.comicReader` is neither read nor written. */
    val readerTutorialSeen: Flow<Boolean> = store.data.map { it[READER_TUTORIAL_SEEN] ?: false }.distinctUntilChanged()

    suspend fun markReaderTutorialSeen() {
        edit { it[READER_TUTORIAL_SEEN] = true }
    }

    /** The tutorial shows again the next time the reader opens. */
    suspend fun resetReaderTutorial() {
        edit { it.remove(READER_TUTORIAL_SEEN) }
    }

    /** Downloads wait for an unmetered network. On by default: volumes are large. */
    val downloadWifiOnly: Flow<Boolean> = store.data.map { it[DOWNLOAD_WIFI_ONLY] ?: true }.distinctUntilChanged()

    suspend fun setDownloadWifiOnly(on: Boolean) {
        edit { it[DOWNLOAD_WIFI_ONLY] = on }
    }

    /** The notification permission was asked for, at the first download. */
    val notificationsAsked: Flow<Boolean> = store.data.map { it[NOTIFICATIONS_ASKED] ?: false }.distinctUntilChanged()

    suspend fun markNotificationsAsked() {
        edit { it[NOTIFICATIONS_ASKED] = true }
    }

    /** The display options of every content grid and Home. Filters and sort stay with each screen. */
    val gridOptions: Flow<GridOptions> = store.data.map { it.gridOptions() }.distinctUntilChanged()

    suspend fun updateGridOptions(change: (GridOptions) -> GridOptions) {
        edit {
            it.dropOldGridKeys()
            it[GRID_OPTIONS] = AppJson.encodeToString(change(it.gridOptions()))
        }
    }

    /** Back to the defaults, every option. */
    suspend fun resetGridOptions() {
        edit {
            it.dropOldGridKeys()
            it.remove(GRID_OPTIONS)
        }
    }

    /** Browse and Facet grids once had options of their own; the shared key keeps the old default group's value. */
    private fun MutablePreferences.dropOldGridKeys() {
        remove(OLD_BROWSE_GRID_OPTIONS)
        remove(OLD_FACET_GRID_OPTIONS)
    }

    /** A failed write (a full disk, or any other I/O error) leaves the stored value, which the controls show; it is logged, not thrown into a viewModelScope. */
    private suspend fun edit(change: (MutablePreferences) -> Unit) {
        try {
            localWrite { store.edit { change(it) } }
        } catch (e: StorageFullException) {
            Log.w("DeviceSettings", "Setting not saved: storage is full", e)
        } catch (e: IOException) {
            Log.w("DeviceSettings", "Setting not saved", e)
        }
    }

    private fun Preferences.gridOptions(): GridOptions {
        val stored = this[GRID_OPTIONS] ?: return GridOptions()
        return try {
            AppJson.decodeFromString(stored)
        } catch (_: SerializationException) {
            GridOptions()
        }
    }

    private companion object {
        /** The old default group's key, kept. */
        val GRID_OPTIONS = stringPreferencesKey("grid_options:default")
        val OLD_BROWSE_GRID_OPTIONS = stringPreferencesKey("grid_options:browse")
        val OLD_FACET_GRID_OPTIONS = stringPreferencesKey("grid_options:facet")
        val THEME = stringPreferencesKey("theme")
        val READER_TUTORIAL_SEEN = booleanPreferencesKey("reader_tutorial_seen")
        val DOWNLOAD_WIFI_ONLY = booleanPreferencesKey("download_wifi_only")
        val NOTIFICATIONS_ASKED = booleanPreferencesKey("notifications_asked")
    }
}
