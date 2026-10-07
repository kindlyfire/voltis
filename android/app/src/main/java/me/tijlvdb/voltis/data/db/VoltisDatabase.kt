package me.tijlvdb.voltis.data.db

import androidx.room.Database
import androidx.room.RoomDatabase

/** One per account (`AccountStores`). Version 1 is edited in place until the first release; after it, every change is a migration. */
@Database(entities = [ReadingSnapshotEntity::class, LaneEntity::class, OpEntity::class, NoticeEntity::class, ContentEntity::class, DownloadEntity::class, CustomListEntity::class, CustomListEntryEntity::class, PendingUserDataEntity::class, SeriesPolicyEntity::class, AutoOfferEntity::class], version = 1, exportSchema = true)
abstract class VoltisDatabase : RoomDatabase() {
    abstract fun snapshots(): SnapshotDao

    abstract fun lanes(): LaneDao

    abstract fun ops(): OpDao

    abstract fun notices(): NoticeDao

    abstract fun content(): ContentDao

    abstract fun downloads(): DownloadDao

    abstract fun lists(): ListDao

    abstract fun pendingUserData(): PendingUserDataDao

    abstract fun auto(): AutoDao
}
