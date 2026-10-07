package me.tijlvdb.voltis.data.reading

import android.content.Context
import androidx.datastore.preferences.preferencesDataStoreFile
import dagger.Binds
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import me.tijlvdb.voltis.data.auth.AppScope
import me.tijlvdb.voltis.data.net.NetworkConnectivity
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.reading.ReadingCommands
import me.tijlvdb.voltis.domain.reading.ReadingSync

@Module
@InstallIn(SingletonComponent::class)
interface ReadingModule {
    @Binds
    fun readingCommands(manager: ReadingSyncManager): ReadingCommands

    @Binds
    fun readingSync(manager: ReadingSyncManager): ReadingSync

    @Binds
    fun connectivity(connectivity: NetworkConnectivity): Connectivity

    companion object {
        /** The DataStore file `writer`, outside the per-account stores: the identity is the install's. Excluded from backup with `datastore/`. */
        @Provides
        @Singleton
        fun writerIdentity(@ApplicationContext context: Context, @AppScope scope: CoroutineScope) = WriterIdentity(
            WriterIdentity.dataStore(CoroutineScope(SupervisorJob() + Dispatchers.IO)) { context.preferencesDataStoreFile("writer") },
            scope,
        )
    }
}
