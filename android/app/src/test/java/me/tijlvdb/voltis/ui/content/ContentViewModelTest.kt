package me.tijlvdb.voltis.ui.content

import androidx.lifecycle.SavedStateHandle
import java.io.IOException
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonElement
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentIds
import me.tijlvdb.voltis.data.api.ContentPage
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ContinueReason
import me.tijlvdb.voltis.data.api.ContinueTarget
import me.tijlvdb.voltis.data.api.ForAccount
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.api.testApi
import me.tijlvdb.voltis.data.auth.testStore
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.content.ContentRepository
import me.tijlvdb.voltis.data.reading.EngineTest
import me.tijlvdb.voltis.data.user.UserRepository
import me.tijlvdb.voltis.domain.reading.CommandOutcome
import me.tijlvdb.voltis.domain.catalog.VolumeReading
import me.tijlvdb.voltis.domain.reading.EffectiveReading
import me.tijlvdb.voltis.domain.reading.EmptyProgress
import me.tijlvdb.voltis.domain.reading.Stamps
import me.tijlvdb.voltis.domain.reading.FakeReadingSync
import me.tijlvdb.voltis.domain.reading.ReadingCommands
import me.tijlvdb.voltis.domain.reading.Shown
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.domain.storage.StorageFullException
import me.tijlvdb.voltis.domain.sync.LandedUserData
import me.tijlvdb.voltis.domain.sync.PendingUserData
import me.tijlvdb.voltis.domain.sync.UserDataItem
import me.tijlvdb.voltis.domain.sync.UserDataSync
import me.tijlvdb.voltis.domain.sync.merge
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.R
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.ResponseBody.Companion.toResponseBody
import retrofit2.HttpException
import retrofit2.Response
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

@OptIn(ExperimentalCoroutinesApi::class)
class ContentViewModelTest {
    @get:Rule
    val tmp = TemporaryFolder()

    /**
     * A caught-up series. The first-volume lookup, the volume list and, when gated, the page's load
     * and the continue target answer when the test says so; all but the load even to a cancelled caller.
     */
    private class Api(real: VoltisApi) : VoltisApi by real {
        var lookups = mutableListOf<CompletableDeferred<List<String>>>()
        var gate: CompletableDeferred<Unit>? = null
        var continues: MutableList<CompletableDeferred<ContinueTarget>>? = null
        var lists = mutableListOf<CompletableDeferred<ContentPage>>()

        var unreachable = false

        /** Answers the page's fetch with this status and Voltis' body instead. */
        var denied: Int? = null

        /** The page's requests, and what the server holds of the item. */
        var gets = 0
        var userData: UserData? = null

        override suspend fun contentById(id: String, account: ForAccount?): Content {
            gets++
            gate?.await()
            if (unreachable) throw IOException()
            denied?.let { throw HttpException(Response.error<Content>(it, "{\"error\": \"Content not found\"}".toResponseBody("application/json".toMediaType()))) }
            return Content(id, "Harbor Lights", ContentType.COMIC_SERIES, childrenCount = 2, userData = userData)
        }

        override suspend fun continueTarget(id: String, account: ForAccount?): ContinueTarget {
            val asked = continues ?: return ContinueTarget(seriesId = id, reason = ContinueReason.CAUGHT_UP)
            return withContext(NonCancellable) { CompletableDeferred<ContinueTarget>().also { asked += it }.await() }
        }

        override suspend fun content(params: Map<String, String>, account: ForAccount?) =
            withContext(NonCancellable) { CompletableDeferred<ContentPage>().also { lists += it }.await() }

        override suspend fun contentIds(params: Map<String, String>) =
            ContentIds(withContext(NonCancellable) { CompletableDeferred<List<String>>().also { lookups += it }.await() })
    }

    /** The pending row of one item, which `merge` builds as the real store does. */
    private class FakeUserData : UserDataSync {
        val rows = MutableStateFlow<PendingUserData?>(null)
        val submits = mutableListOf<Pair<Boolean?, JsonElement?>>()
        var seq = 0L

        /** No owner is bound. */
        var unavailable = false
        var full = false

        override suspend fun setUserData(account: String, item: UserDataItem, starred: Boolean?, rating: JsonElement?) {
            if (full) throw StorageFullException()
            submits += starred to rating
            rows.value = rows.value.merge(item, starred, rating, 0)
        }

        override fun pending(account: String, contentId: String): Flow<PendingUserData?> = rows

        /** A database closed under the read (a sign-out). */
        var closed = false

        override suspend fun landedSeq(account: String): Long = when {
            unavailable -> throw SyncUnavailable.AccountChanged()
            closed -> throw IllegalStateException("database closed")
            else -> seq
        }

        /** The row after a write landed: no wish, the server's answer at [at]. */
        fun landed(at: Long, starred: Boolean, rating: Int?) {
            rows.value = PendingUserData("c_series", null, null, "t", false, null, false, null, 1, 0, null, 0, LandedUserData(at, starred, rating))
        }
    }

    private class Commands : ReadingCommands {
        var fail = false

        /** Each command, with the content it was made with. */
        val made = mutableListOf<List<Any?>>()

        private fun made(vararg call: Any?) = CommandOutcome.QUEUED.also { made += call.toList() }

        override suspend fun clear(content: Content) = made("clear", content).also { check(!fail) }

        override suspend fun setStatus(content: Content, status: String?) = made("set_status", content, status)

        override suspend fun markSeriesCompleted(series: Content, includeUnread: Boolean) = made("mark_series_completed", series)

        override suspend fun markThrough(series: Content, untilId: String) = made("mark_through", series, untilId)
    }

    private val logged = mutableListOf<Throwable>()

    private fun TestScope.viewModel(
        api: Api,
        commands: Commands,
        sync: FakeReadingSync = FakeReadingSync(),
        cached: List<Content>? = null,
        stored: Map<String, Content> = emptyMap(),
        connectivity: EngineTest.FakeConnectivity = EngineTest.FakeConnectivity { true },
        userData: FakeUserData = FakeUserData(),
        storeFails: Set<String> = emptySet(),
        readings: Flow<List<VolumeReading>?> = flowOf(null),
        effective: Flow<Map<String, EffectiveReading>> = flowOf(emptyMap()),
        known: Map<String, Content> = emptyMap(),
        forgotten: MutableList<String> = mutableListOf(),
        /** What the last page showed of each item's reading, and what this page shows afterwards. */
        hints: MutableMap<String, UserData?> = mutableMapOf(),
    ): ContentViewModel {
        val events = CatalogEvents()
        val users = UserRepository(api, testStore(tmp.newFolder().resolve("s.preferences_pb"), backgroundScope), events, backgroundScope)
        return ContentViewModel(
            "c_series", SavedStateHandle(), ContentRepository(api), commands, sync, userData, "a", { cached }, { if (it in storeFails) throw IOException("disk") else stored[it] }, flowOf(true), connectivity, users, events,
            cachedSeries = { readings }, effective = { effective }, first = FirstFrame(known::get, { null }, {}, { forgotten += it }, hints::containsKey, hints::get, { id, shown -> hints[id] = shown }), log = { _, e -> logged += e },
        )
    }

    /** With the server cut after the page loaded: what is unsent shows, the page stays, and Update progress works from the cached volumes. */
    @Test
    fun unsentShowsAndTheServerCutKeepsThePage() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val api = Api(testApi())
        val commands = Commands()
        val sync = FakeReadingSync()
        val volumes = listOf(Content("c_vol1", "Vol. 1", ContentType.COMIC, parentId = "c_series"), Content("c_vol2", "Vol. 2", ContentType.COMIC, parentId = "c_series"))
        val effective = MutableStateFlow(emptyMap<String, EffectiveReading>())
        fun reading(status: String?) = EffectiveReading(status, EmptyProgress, null, Stamps(null, null, null, null), seq = 1)
        val vm = viewModel(api, commands, sync, cached = volumes, effective = effective)
        runCurrent()
        val fetched = vm.content!!
        assertEquals(null, fetched.userData)

        // A lane's own projection (here an older held base, with an op of its own) is review bookkeeping: it never shows as reading.
        val lane = Shown("on_hold", EmptyProgress, null, unsent = true, needsReview = true, projected = true)
        sync.shownRows.value = mapOf("c_series" to lane, "c_vol1" to lane.copy(status = "dropped"))
        runCurrent()
        assertEquals(listOf(null, true), listOf(vm.content?.userData?.status, vm.needsReview))
        // What shows is the effective reading, of the item, its series and the listed volumes, as it changes.
        effective.value = mapOf("c_series" to reading("reading"), "c_vol1" to reading("completed"))
        runCurrent()
        assertEquals("reading", vm.content?.userData?.status)

        // A reload that doesn't reach the server changes nothing, and shows no error.
        api.unreachable = true
        testScheduler.advanceTimeBy(1000)
        vm.refresh()
        runCurrent()
        assertEquals(listOf("reading", null, false), listOf(vm.content?.userData?.status, vm.error, vm.refreshing))

        // The picker lists the cached volumes, with what is unsent for them; Mark all finds the last one there.
        vm.show(ContentDialog.UpdateProgress)
        vm.loadVolumes()
        runCurrent()
        api.lists.single().completeExceptionally(IOException())
        runCurrent()
        assertEquals(listOf("c_vol1" to "completed", "c_vol2" to null), vm.volumes?.map { it.id to it.userData?.status })
        vm.markThrough(null)
        runCurrent()
        api.lookups.single().completeExceptionally(IOException())
        runCurrent()
        // The command is made with the row as fetched: the lane is seeded from the stored snapshot, not from what shows.
        assertEquals(listOf(listOf("mark_through", fetched, "c_vol2")), commands.made)
        assertEquals(listOf(null, false, "reading"), listOf(vm.dialog, vm.busy, vm.content?.userData?.status))

        vm.viewModelScope.cancel()
    }

    /** A page opened from a card has the card's row on its first frame; the fetch replaces it, and no command is made on the row before. */
    @Test
    fun knownRowShowsBeforeTheFetch() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val api = Api(testApi()).apply { gate = CompletableDeferred() }
        val commands = Commands()
        val listed = Content("c_series", "Harbor Lights (listed)", ContentType.COMIC_SERIES, childrenCount = 5)
        // The last page showed it starred; the item's pending star was discarded since.
        val hints = mutableMapOf<String, UserData?>("c_series" to UserData(starred = true))
        val vm = viewModel(api, commands, known = mapOf("c_series" to listed), hints = hints)
        // Before anything ran: the page's first frame.
        assertEquals(listOf(listed.copy(userData = UserData(starred = true)), null), listOf(vm.content, vm.parent))
        runCurrent()
        // The pending flow and the effective readings have answered (nothing pending): the hint is gone, the memo row's own data shows.
        assertEquals(listed, vm.content)
        // Star and rating wait for the fetched row, like the other controls.
        assertEquals(false, vm.ready)
        vm.setStatus("reading")
        runCurrent()
        assertEquals(0, commands.made.size)

        api.gate!!.complete(Unit)
        runCurrent()
        assertEquals(listOf("Harbor Lights", false, true), listOf(vm.content?.title, vm.busy, vm.ready))
        // What was shown is remembered for display only, once the page is complete.
        assertEquals(listOf<UserData?>(null), listOf(hints["c_series"]))
        vm.viewModelScope.cancel()
    }

    /** The fetch lands before the readings answer: the hint still shows, the fetched row stays the command base, and the flows win when they arrive. */
    @Test
    fun hintOutlivesAFastFetchUntilTheFlowsAnswer() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val effective = MutableSharedFlow<Map<String, EffectiveReading>>()
        val hints = mutableMapOf<String, UserData?>("c_series" to UserData(starred = true))
        val listed = Content("c_series", "Harbor Lights (listed)", ContentType.COMIC_SERIES)
        val vm = viewModel(Api(testApi()), Commands(), known = mapOf("c_series" to listed), hints = hints, effective = effective)
        runCurrent()
        assertEquals(listOf("Harbor Lights", true, true), listOf(vm.content?.title, vm.ready, vm.content?.userData?.starred))
        effective.emit(emptyMap())
        runCurrent()
        assertEquals(false, vm.content?.userData?.starred ?: false)
        vm.viewModelScope.cancel()
    }

    /** The server says the item is gone after the page loaded: nothing of it stays, and no command can be made. */
    @Test
    fun missingAfterASuccessfulLoadInvalidates() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val api = Api(testApi())
        val forgotten = mutableListOf<String>()
        val vm = viewModel(api, Commands(), forgotten = forgotten)
        runCurrent()
        assertEquals(listOf(true, null), listOf(vm.ready, vm.error))
        api.denied = 404
        testScheduler.advanceTimeBy(1000)
        vm.refresh()
        runCurrent()
        assertEquals(listOf<Any?>(null, false, null, listOf("c_series")), listOf(vm.content, vm.ready, vm.next, forgotten))
        assertNotNull(vm.error)
        vm.viewModelScope.cancel()
    }

    /** The server says a seeded item is gone: it is evicted and not shown from memory; a transient failure keeps it. */
    @Test
    fun deniedAfterSeedEvictsIt() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val listed = Content("c_series", "Harbor Lights (listed)", ContentType.COMIC_SERIES, childrenCount = 5)
        val kept = viewModel(Api(testApi()).apply { unreachable = true }, Commands(), known = mapOf("c_series" to listed))
        runCurrent()
        assertEquals(listOf(listed, false), listOf(kept.content, kept.busy))

        val forgotten = mutableListOf<String>()
        val vm = viewModel(Api(testApi()).apply { denied = 404 }, Commands(), known = mapOf("c_series" to listed), forgotten = forgotten)
        runCurrent()
        assertEquals(listOf<Any?>(null, false, listOf("c_series")), listOf(vm.content, vm.ready, forgotten))
        assertNotNull(vm.error)
        kept.viewModelScope.cancel()
        vm.viewModelScope.cancel()
    }

    /** Offline the page is what is stored, without asking the server; with nothing stored it is the offline state, until the server is back. */
    @Test
    fun offlineFromWhatIsStored() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val api = Api(testApi())
        val commands = Commands()
        val connectivity = EngineTest.FakeConnectivity { false }.apply { online.value = false }
        val series = Content("c_series", "Harbor Lights", ContentType.COMIC_SERIES, childrenCount = 2)
        api.gate = CompletableDeferred()
        val vm = viewModel(api, commands, stored = mapOf("c_series" to series), connectivity = connectivity)
        runCurrent()
        assertEquals(listOf(series, true, false, null), listOf(vm.content, vm.cached, vm.unavailable, vm.next))
        vm.setStatus("reading")
        runCurrent()
        assertEquals(listOf(listOf("set_status", series, "reading")), commands.made)

        // Its continue target follows the stored volumes' readings as they change; none while they aren't known.
        val readings = MutableStateFlow<List<VolumeReading>?>(null)
        fun reading(n: Int, status: String?) = VolumeReading(
            Content("c_vol$n", "Vol. $n", ContentType.COMIC, parentId = "c_series"),
            EffectiveReading(status, EmptyProgress, null, Stamps(null, null, null, null), seq = 1),
        )
        val offline = viewModel(api, commands, stored = mapOf("c_series" to series), connectivity = connectivity, readings = readings)
        runCurrent()
        assertEquals(null, offline.next)
        readings.value = listOf(reading(1, "completed"), reading(2, null))
        runCurrent()
        assertEquals("c_vol2", offline.next?.target?.id)
        readings.value = listOf(reading(1, "completed"), reading(2, "completed"))
        runCurrent()
        assertEquals(listOf(null, "caught_up"), listOf(offline.next?.target?.id, offline.next?.reason))
        readings.value = null
        runCurrent()
        assertEquals(null, offline.next)

        val bare = viewModel(api, commands, connectivity = connectivity)
        runCurrent()
        assertEquals(listOf(null, true), listOf(bare.content, bare.unavailable))
        connectivity.online.value = true
        bare.refresh()
        api.gate?.complete(Unit)
        runCurrent()
        assertEquals(listOf("Harbor Lights", false, false), listOf(bare.content?.title, bare.unavailable, bare.cached))

        // A read of the store that fails is the page's error and Retry, not a crash of the launch.
        val broken = viewModel(api, commands, connectivity = EngineTest.FakeConnectivity { false }.apply { online.value = false }, storeFails = setOf("c_series"))
        runCurrent()
        assertEquals(listOf(null, false), listOf(broken.content, broken.refreshing))
        assertNotNull(broken.error)
        // The item reads and its series doesn't: the item shows, without the link, and nothing is stuck.
        val volume = Content("c_series", "Vol 1", ContentType.COMIC, parentId = "c_parent")
        val orphan = viewModel(
            api, commands, stored = mapOf("c_series" to volume), storeFails = setOf("c_parent"),
            connectivity = EngineTest.FakeConnectivity { false }.apply { online.value = false },
        )
        runCurrent()
        assertEquals(listOf(volume, null, null), listOf(orphan.content, orphan.parent, orphan.error))
        orphan.viewModelScope.cancel()

        vm.viewModelScope.cancel()
        bare.viewModelScope.cancel()
        broken.viewModelScope.cancel()
    }

    /** Star and rating show from the item's one row, never from the order of loads and events (P4 §8). */
    @Test
    fun starAndRatingShowFromTheirRow() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val api = Api(testApi())
        val userData = FakeUserData()
        val effects = mutableListOf<ContentEffect>()
        val vm = viewModel(api, Commands(), userData = userData)
        backgroundScope.launch { vm.effects.toList(effects) }
        runCurrent()
        fun shown() = (vm.content?.userData?.starred ?: false) to vm.content?.userData?.rating
        val gets = api.gets

        // A submit shows at once, with no reload and no busy, and no wait for the server.
        vm.setStarred(true)
        vm.rate(4)
        runCurrent()
        assertEquals(listOf(true to 4, false, gets), listOf(shown(), vm.busy, api.gets))
        assertEquals(2, userData.submits.size)
        // Discard, or a drop: the loaded values, whatever arrives first.
        userData.rows.value = null
        runCurrent()
        assertEquals(false to null, shown())

        // No owner bound: the load goes on, and installs what the server holds.
        userData.unavailable = true
        api.userData = UserData(starred = true, rating = 2)
        vm.refresh()
        runCurrent()
        assertEquals(true to 2, shown())
        // A closed database fails the stamp's read, not the page.
        userData.unavailable = false
        userData.closed = true
        api.userData = UserData(starred = false, rating = 3)
        vm.refresh()
        runCurrent()
        assertEquals(false to 3, shown())
        assertEquals(1, logged.size)
        userData.closed = false

        // A landing newer than the load's stamp shows over the load, also over a failing reload.
        userData.unavailable = false
        api.userData = UserData(starred = false)
        userData.landed(at = 1, starred = true, rating = null)
        vm.refresh()
        runCurrent()
        assertEquals(true to null, shown())
        // A rating submitted afterwards keeps the landed star.
        vm.rate(5)
        api.unreachable = true
        vm.refresh()
        runCurrent()
        assertEquals(true to 5, shown())
        // A load stamped after the landing installs the main row: the server's star, and the wish over its rating.
        api.unreachable = false
        userData.seq = 1
        vm.refresh()
        runCurrent()
        assertEquals(false to 5, shown())

        // A refused submit says so and changes nothing.
        userData.full = true
        effects.clear()
        vm.setStarred(true)
        runCurrent()
        assertEquals(listOf<ContentEffect>(ContentEffect.Message(UiText.Res(R.string.error_storage_full))), effects)
        assertEquals(false to 5, shown())

        // A page served from storage takes them too.
        val connectivity = EngineTest.FakeConnectivity { false }.apply { online.value = false }
        val stored = viewModel(api, Commands(), stored = mapOf("c_series" to Content("c_series", "Harbor Lights", ContentType.COMIC_SERIES)), connectivity = connectivity)
        runCurrent()
        stored.setStarred(true)
        runCurrent()
        assertEquals(true to true, stored.cached to stored.content?.userData?.starred)

        vm.viewModelScope.cancel()
        stored.viewModelScope.cancel()
    }

    @Test
    fun lateAnswersChangeNothing() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val api = Api(testApi())
        val commands = Commands()
        val vm = viewModel(api, commands)
        val effects = mutableListOf<ContentEffect>()
        backgroundScope.launch { vm.effects.toList(effects) }

        // The first continue request fails. Its Retry is still out when a write reloads the page:
        // the reload's answer stands, and the Retry's late one is dropped.
        val asked = mutableListOf<CompletableDeferred<ContinueTarget>>()
        api.continues = asked
        runCurrent()
        asked[0].completeExceptionally(IllegalStateException())
        runCurrent()
        assertEquals(null to true, vm.next to vm.nextFailed)
        vm.retryNext()
        runCurrent()
        vm.setStatus("reading")
        runCurrent()
        assertEquals(3, asked.size)
        val caughtUp = ContinueTarget(seriesId = "c_series", reason = ContinueReason.CAUGHT_UP)
        asked[2].complete(caughtUp)
        runCurrent()
        asked[1].complete(ContinueTarget(action = "start"))
        runCurrent()
        assertEquals(Triple(caughtUp, false, false), Triple(vm.next, vm.nextFailed, vm.busy))
        api.continues = null
        effects.clear()

        // A dialog opened during Read again's lookup keeps the screen: the lookup's answer opens nothing over it.
        // Dismissed during its list request and reopened, it asks for the list again and ignores the first request's
        // late failure.
        vm.readAgain()
        runCurrent()
        vm.show(ContentDialog.UpdateProgress)
        api.lookups.removeAt(0).complete(listOf("c_vol1"))
        vm.loadVolumes()
        runCurrent()
        assertEquals(ContentDialog.UpdateProgress, vm.dialog)
        vm.dismiss()
        vm.show(ContentDialog.UpdateProgress)
        vm.loadVolumes()
        runCurrent()
        assertEquals(2, api.lists.size)
        api.lists[0].completeExceptionally(IllegalStateException())
        runCurrent()
        assertEquals(null to null, vm.volumes to vm.volumesError)
        api.lists[1].complete(ContentPage(emptyList()))
        runCurrent()
        assertEquals(emptyList<Content>(), vm.volumes)
        vm.dismiss()

        // A second tap during the lookup starts nothing. The lookup fails: no dialog, and Retry looks again.
        vm.readAgain()
        vm.readAgain()
        runCurrent()
        assertEquals(1, api.lookups.size)
        api.lookups[0].completeExceptionally(IllegalStateException())
        runCurrent()
        assertEquals(null to true, vm.dialog to vm.nextFailed)
        vm.retryNext()
        runCurrent()
        api.lookups[1].complete(listOf("c_vol1"))
        runCurrent()
        assertEquals(ContentDialog.Clear("c_vol1"), vm.dialog)

        // A failed clear stays in its dialog and opens nothing.
        commands.fail = true
        vm.clear()
        runCurrent()
        assertEquals(ContentDialog.Clear("c_vol1"), vm.dialog)
        assertEquals(true to false, (vm.dialogError != null) to vm.busy)
        assertEquals(emptyList<ContentEffect>(), effects)

        // A clear that worked opens the reader; the page stays busy until its reload has landed.
        commands.fail = false
        api.gate = CompletableDeferred()
        vm.clear()
        runCurrent()
        assertEquals(null to true, vm.dialog to vm.busy)
        assertEquals(ContentEffect.Read("c_vol1"), effects.last())
        assertEquals(2, effects.size)
        api.gate!!.complete(Unit)
        runCurrent()
        assertEquals(false, vm.busy)

        vm.viewModelScope.cancel()
    }

    @After
    fun tearDown() = Dispatchers.resetMain()
}
