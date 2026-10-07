package me.tijlvdb.voltis.data.settings

import android.util.Log
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import java.io.IOException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.storage.localWrite
import me.tijlvdb.voltis.domain.comic.ReaderSettings
import me.tijlvdb.voltis.domain.storage.StorageFullException

/** The comic reader's settings: per device, one JSON string per server. */
@Singleton
class ReaderSettingsStore @Inject constructor(
    @param:DeviceStore private val store: DataStore<Preferences>,
    private val sessions: SessionStore,
) {
    @OptIn(ExperimentalCoroutinesApi::class)
    val settings: Flow<ReaderSettings> = sessions.state
        .map { it.server?.id }
        .distinctUntilChanged()
        .flatMapLatest { server ->
            if (server == null) return@flatMapLatest flowOf(ReaderSettings())
            store.data.map { ReaderSettings.parse(it[key(server)]) }
        }
        .distinctUntilChanged()

    /**
     * Changes the current server's settings, and returns them as stored; null without a server. On a failed
     * write (a full disk, or another I/O error) nothing changes, and the stored settings are returned.
     */
    suspend fun update(change: (ReaderSettings) -> ReaderSettings): ReaderSettings? {
        val key = key(sessions.active()?.server?.id ?: return null)
        var updated: ReaderSettings? = null
        try {
            localWrite {
                store.edit { prefs ->
                    updated = change(ReaderSettings.parse(prefs[key])).also { prefs[key] = it.encode() }
                }
            }
        } catch (e: StorageFullException) {
            Log.w("ReaderSettings", "Settings not saved: storage is full", e)
            return stored(key)
        } catch (e: IOException) {
            Log.w("ReaderSettings", "Settings not saved", e)
            return stored(key)
        }
        return updated
    }

    /** The settings as stored; the defaults when even the read fails. */
    private suspend fun stored(key: Preferences.Key<String>): ReaderSettings =
        try {
            ReaderSettings.parse(store.data.first()[key])
        } catch (e: IOException) {
            ReaderSettings()
        }

    private fun key(server: String) = stringPreferencesKey("reader_comics:$server")
}
