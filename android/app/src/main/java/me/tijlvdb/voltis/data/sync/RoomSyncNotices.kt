package me.tijlvdb.voltis.data.sync

import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.db.AccountStore
import me.tijlvdb.voltis.data.db.AccountStores
import me.tijlvdb.voltis.data.db.NoticeEntity
import me.tijlvdb.voltis.data.db.OpenAccount
import me.tijlvdb.voltis.domain.sync.NoticeDetail
import me.tijlvdb.voltis.domain.sync.StoredNotice
import me.tijlvdb.voltis.domain.sync.SyncNotices

/** [SyncNotices] over the open account's `sync_notice`. Without an open account there are none, and nothing can be stored. */
@Singleton
class RoomSyncNotices @Inject constructor(private val stores: AccountStores) : SyncNotices {
    /** No replay: a screen that wasn't collecting doesn't get old notices. [announce] suspends rather than drop one. */
    private val _fresh = MutableSharedFlow<StoredNotice>(extraBufferCapacity = 16)
    override val fresh = _fresh.asSharedFlow()

    override suspend fun insert(store: OpenAccount, notice: StoredNotice, announce: Boolean) {
        check(store is AccountStore && stores.current.value === store) { "The account's store is closed" }
        val dao = store.db.notices()
        val stored = notice.copy(id = dao.insert(notice.toEntity()))
        if (announce) announce(listOf(stored))
    }

    @OptIn(ExperimentalCoroutinesApi::class)
    override fun observe(): Flow<List<StoredNotice>> = stores.current.flatMapLatest { store ->
        store?.db?.notices()?.observe()?.map { rows -> rows.map { it.toNotice() } } ?: flowOf(emptyList())
    }

    /** Notices stored in another writer's transaction (the reading engine's), once it committed. */
    suspend fun announce(notices: List<StoredNotice>) {
        for (notice in notices) _fresh.emit(notice)
    }
}

fun StoredNotice.toEntity() =
    NoticeEntity(id, contentId, title, kind, AppJson.encodeToString(NoticeDetail.serializer(), detail), createdAt)

fun NoticeEntity.toNotice() = StoredNotice(
    id, contentId, title, kind,
    runCatching { AppJson.decodeFromString(NoticeDetail.serializer(), detail) }.getOrDefault(NoticeDetail()),
    createdAt,
)

@Module
@InstallIn(SingletonComponent::class)
interface SyncModule {
    @Binds
    fun syncNotices(notices: RoomSyncNotices): SyncNotices
}
