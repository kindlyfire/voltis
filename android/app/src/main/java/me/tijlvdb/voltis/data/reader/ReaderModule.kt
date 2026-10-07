package me.tijlvdb.voltis.data.reader

import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import me.tijlvdb.voltis.domain.comic.ReaderData

@Module
@InstallIn(SingletonComponent::class)
interface ReaderModule {
    @Binds
    fun readerData(data: OfflineFirstReaderData): ReaderData
}
