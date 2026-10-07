package me.tijlvdb.voltis.ui.nav

import androidx.activity.ComponentActivity
import androidx.activity.compose.LocalActivity
import androidx.compose.animation.ExperimentalSharedTransitionApi
import androidx.compose.animation.SharedTransitionLayout
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.LocalViewModelStoreOwner
import androidx.lifecycle.viewmodel.compose.rememberViewModelStoreOwner
import androidx.lifecycle.viewmodel.navigation3.rememberViewModelStoreNavEntryDecorator
import androidx.navigation3.runtime.NavEntry
import androidx.navigation3.runtime.rememberSaveableStateHolderNavEntryDecorator
import androidx.navigation3.ui.NavDisplay
import kotlinx.coroutines.flow.StateFlow
import me.tijlvdb.voltis.data.auth.SessionState
import me.tijlvdb.voltis.data.content.ContinueOpen
import me.tijlvdb.voltis.data.content.GridFilters
import me.tijlvdb.voltis.ui.content.ContentScreen
import me.tijlvdb.voltis.ui.entryInsets
import me.tijlvdb.voltis.ui.discover.DiscoverScreen
import me.tijlvdb.voltis.ui.discover.FacetScreen
import me.tijlvdb.voltis.ui.downloads.AttentionScreen
import me.tijlvdb.voltis.ui.downloads.DownloadsScreen
import me.tijlvdb.voltis.ui.grid.GridScreen
import me.tijlvdb.voltis.ui.grid.GridSource
import me.tijlvdb.voltis.ui.home.HomeScreen
import me.tijlvdb.voltis.ui.libraries.LibrariesScreen
import me.tijlvdb.voltis.ui.lists.ListScreen
import me.tijlvdb.voltis.ui.lists.ListsScreen
import me.tijlvdb.voltis.ui.login.LoginScreen
import me.tijlvdb.voltis.ui.reader.ReaderScreen
import me.tijlvdb.voltis.ui.search.SearchScreen
import me.tijlvdb.voltis.ui.server.ServerScreen
import me.tijlvdb.voltis.ui.settings.DownloadSettingsScreen
import me.tijlvdb.voltis.ui.settings.LibraryVisibilityScreen
import me.tijlvdb.voltis.ui.settings.ReaderDefaultsScreen
import me.tijlvdb.voltis.ui.settings.SessionsScreen
import me.tijlvdb.voltis.ui.settings.SettingsScreen

/**
 * The session state picks the screen; screens change it rather than navigating themselves. A launch
 * target waits while the session loads, is applied once signed in, and is dropped otherwise.
 */
@Composable
fun AppNav(session: StateFlow<SessionState>, targets: LaunchTargets, resolveContinue: suspend () -> ContinueOpen?) {
    val state by session.collectAsStateWithLifecycle()
    val current = state
    if (current == SessionState.Loading) {
        Surface(Modifier.fillMaxSize()) {}
        return
    }
    val nav = rememberSaveable(saver = MainNavState.Saver) { MainNavState() }
    // Saved with the stacks: a restored back stack has had its landing.
    val landing = rememberSaveable { mutableStateOf(true) }
    val user = current.account
    var navUser by rememberSaveable { mutableStateOf(user) }
    // Any session change (sign-out, a 401, another user) starts over on Home. Loading returned
    // above, so stacks restored after process death are kept.
    LaunchedEffect(user) {
        if (navUser != null && navUser != user) {
            nav.clear()
            // A fresh stack decides its landing again (offline, a new session opens on Downloads).
            landing.value = true
        }
        navUser = user
    }
    when (current) {
        SessionState.NoServer -> Scoped { ServerScreen() }
        is SessionState.SignedIn -> MainNav(nav, current.account, targets, resolveContinue, landing)
        // SignedOut or NeedsReauth.
        else -> key(current.server?.id) { Scoped { LoginScreen() } }
    }
    if (current !is SessionState.SignedIn) {
        // Signing in afterwards doesn't go there.
        val pending by targets.pending.collectAsStateWithLifecycle()
        LaunchedEffect(pending) { pending?.let(targets::take) }
    }
}

/** Gives [content] view models of its own, cleared when it leaves: a login form mustn't outlive its server. */
@Composable
private fun Scoped(content: @Composable () -> Unit) {
    CompositionLocalProvider(LocalViewModelStoreOwner provides rememberViewModelStoreOwner(), content = content)
}

@OptIn(ExperimentalSharedTransitionApi::class)
@Composable
private fun MainNav(nav: MainNavState, account: String, targets: LaunchTargets, resolveContinue: suspend () -> ContinueOpen?, landing: MutableState<Boolean>) {
    // The offline landing on Downloads gives way while a launch target is pending.
    OfflineLanding(nav, landing, blocked = { targets.pending.value != null })
    val pending by targets.pending.collectAsStateWithLifecycle()
    // Resolved, then taken: a newer target or a recreated activity cancels this while the target stays pending.
    LaunchedEffect(pending) {
        val target = pending ?: return@LaunchedEffect
        val fresh = nav.atRoots
        val open = if (target == LaunchTarget.Continue) resolveContinue() else null
        if (!targets.take(target)) return@LaunchedEffect
        when (target) {
            LaunchTarget.Search -> {
                nav.closeReader()
                nav.select(Tab.Search)
            }
            // As the download notification always has.
            LaunchTarget.Downloads -> nav.showInLibraries(DownloadsDest())
            LaunchTarget.Continue -> when (open) {
                is ContinueOpen.Reader -> nav.openReader(open.contentId)
                is ContinueOpen.Page -> nav.closeReader(thenOpen = ContentDest(open.contentId))
                // In place of the offline landing this target replaced.
                ContinueOpen.NothingOffline -> if (fresh && nav.atRoots) nav.showInLibraries(DownloadsDest(landing = true))
                null -> {}
            }
        }
    }
    val transitions = rememberNavTransitions()
    val taps = remember { HeroTaps() }
    MainScaffold(nav, account) {
        SharedTransitionLayout {
        CompositionLocalProvider(LocalSharedTransitionScope provides this, LocalHeroTaps provides taps) {
        NavDisplay(
            backStack = nav.entries,
            // Not called at the Home root, where Back is the system's (HomeRootSceneStrategy).
            onBack = nav::back,
            // An entry's rememberSaveable state and view models live until it leaves the list.
            entryDecorators = listOf(
                rememberSaveableStateHolderNavEntryDecorator(),
                rememberViewModelStoreNavEntryDecorator(),
            ),
            sceneStrategies = listOf(HomeRootSceneStrategy),
            sharedTransitionScope = this,
            transitionSpec = transitions.push,
            popTransitionSpec = transitions.pop,
            predictivePopTransitionSpec = transitions.predictivePop,
            entryProvider = { entry ->
                val tab = nav.tabOf(entry)
                val metadata = buildMap<String, Any> {
                    if (tab != null) put(TAB_KEY, tab)
                    if (entry.dest == HomeDest) putAll(HomeRootMetadata)
                }
                NavEntry(entry, metadata = metadata) {
                    // The reader draws over the whole window; every other screen keeps clear of the side insets.
                    if (it.dest is ReaderDest) Destination(it, nav) else Box(Modifier.entryInsets()) { Destination(it, nav) }
                }
            },
        )
        }
        }
    }
}

/**
 * A fresh back stack opens on Downloads when the server can't be reached at the start (P2 §10). It
 * waits for the start's check, and gives way to anything that moved the stack meanwhile (a tap, a
 * request from an intent) or is [blocked] when it decides (a launch target still resolving).
 * Decided once per fresh stack.
 */
@Composable
private fun OfflineLanding(nav: MainNavState, pending: MutableState<Boolean>, blocked: () -> Boolean) {
    if (!pending.value) return
    val activity = LocalActivity.current as ComponentActivity
    val scaffold = hiltViewModel<ScaffoldViewModel>(activity)
    LaunchedEffect(Unit) {
        if (scaffold.offlineAtStart() && nav.atRoots && !blocked()) nav.showInLibraries(DownloadsDest(landing = true))
        pending.value = false
    }
}

@Composable
private fun Destination(entry: Entry, nav: MainNavState) {
    val taps = LocalHeroTaps.current
    val openContent = { id: String -> nav.open(ContentDest(id, taps.take(id))) }
    CompositionLocalProvider(
        LocalHeroOrigin provides entry.uid.toString(),
        LocalHeroTarget provides (entry.dest as? ContentDest)?.hero,
    ) { Screen(entry, nav, openContent) }
}

@Composable
private fun Screen(entry: Entry, nav: MainNavState, openContent: (String) -> Unit) {
    val back = nav::back
    when (val dest = entry.dest) {
        HomeDest -> HomeScreen(nav::openReader, openContent, browse = { nav.open(BrowseDest(it, GridFilters.DESC)) })
        LibrariesDest -> LibrariesScreen(
            openLibrary = { nav.open(LibraryDest(it)) },
            browse = { nav.open(BrowseDest()) },
            discover = { nav.open(DiscoverDest) },
            downloads = { nav.open(DownloadsDest()) },
            lists = { nav.open(ListsDest) },
        )
        SearchDest -> SearchScreen(openContent)
        SettingsDest -> SettingsScreen(
            openSessions = { nav.open(SessionsDest) },
            openReaderDefaults = { nav.open(ReaderDefaultsDest) },
            openLibraryVisibility = { nav.open(LibraryVisibilityDest) },
            openDownloadSettings = { nav.open(DownloadSettingsDest) },
        )
        is ReaderDest -> ReaderScreen(
            dest.contentId,
            onVolume = nav::openReader,
            onClose = { nav.closeReader(thenOpen = it?.let(::ContentDest)) },
        )
        is LibraryDest -> GridScreen(GridSource.Library(dest.id), back, openContent, nav::openReader)
        is BrowseDest -> GridScreen(GridSource.Browse(dest.sort, dest.sortOrder), back, openContent, nav::openReader)
        is ContentDest -> ContentScreen(
            dest.id, back, openContent, nav::openReader,
            openFacet = { nav.open(FacetDest(it.kind, it.key)) },
            openDownloads = { nav.open(DownloadsDest(it)) },
        )
        is DownloadsDest -> DownloadsScreen(back, openContent, nav::openReader, openAttention = { nav.open(AttentionDest) }, seriesId = dest.seriesId,
            // Only while nothing is above it but the Libraries root: from anywhere deeper Back has somewhere to go.
            landing = dest.landing && nav.stacks.getValue(Tab.Libraries).size <= 2,
        )
        AttentionDest -> AttentionScreen(back)
        ListsDest -> ListsScreen(back, openList = { nav.open(ListDest(it)) })
        is ListDest -> ListScreen(dest.id, back, openContent, openLibrary = { nav.open(LibraryDest(it)) })
        DiscoverDest -> DiscoverScreen(back, openFacet = { kind, key, libraryId -> nav.open(FacetDest(kind, key, libraryId)) })
        is FacetDest -> FacetScreen(dest.kind, dest.key, dest.libraryId, back, openContent, nav::openReader)
        SessionsDest -> SessionsScreen(back)
        ReaderDefaultsDest -> ReaderDefaultsScreen(back)
        LibraryVisibilityDest -> LibraryVisibilityScreen(back)
        DownloadSettingsDest -> DownloadSettingsScreen(back)
    }
}
