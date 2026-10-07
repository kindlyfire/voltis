package me.tijlvdb.voltis.ui.home

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlin.math.ceil
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.content.ContentSort
import me.tijlvdb.voltis.ui.RefreshOnResume
import me.tijlvdb.voltis.ui.ServerOffline
import me.tijlvdb.voltis.ui.isOnline
import me.tijlvdb.voltis.ui.bottomSpace
import me.tijlvdb.voltis.ui.grid.ContentCard
import me.tijlvdb.voltis.ui.grid.ContentCardSkeleton
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.ScrollRow
import me.tijlvdb.voltis.ui.kit.ScrollRowGap
import me.tijlvdb.voltis.ui.kit.scrollRowItemWidth
import me.tijlvdb.voltis.ui.pageGutter
import me.tijlvdb.voltis.ui.topInset

/** [browse] opens All libraries with the given sort, descending. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomeScreen(
    openReader: (contentId: String) -> Unit,
    openContent: (contentId: String) -> Unit,
    browse: (sort: String) -> Unit,
    vm: HomeViewModel = hiltViewModel(),
) {
    RefreshOnResume(vm.refresher)
    val me by vm.me.collectAsStateWithLifecycle()
    Surface(Modifier.fillMaxSize()) {
        PullToRefreshBox(vm.refreshing, vm::refresh, Modifier.topInset()) {
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = bottomSpace())) {
                Text(
                    stringResource(R.string.app_name),
                    Modifier.padding(horizontal = pageGutter(), vertical = 12.dp).semantics { heading() },
                    style = MaterialTheme.typography.titleLarge,
                )
                val user = me
                if (!isOnline()) {
                    ServerOffline()
                } else if (vm.libraries?.isEmpty() == true && user != null) {
                    NoLibraries(admin = user.isAdmin)
                } else {
                    Rows(vm, openReader, openContent, browse)
                }
            }
        }
    }
}

@Composable
private fun Rows(vm: HomeViewModel, openReader: (String) -> Unit, openContent: (String) -> Unit, browse: (String) -> Unit) {
    val itemWidth = scrollRowItemWidth()
    val card = Modifier.width(itemWidth)
    val options by vm.options.collectAsStateWithLifecycle()
    val badges by vm.badges.collectAsStateWithLifecycle(emptyMap())
    Column(verticalArrangement = Arrangement.spacedBy(28.dp)) {
        val reading = vm.reading
        RowError(reading, vm::refresh)
        if (reading.loading || !reading.items.isNullOrEmpty()) {
            ScrollRow(
                stringResource(R.string.home_continue),
                onSeeAll = { browse(ContentSort.CONTINUE) },
                placeholder = skeletons(reading.loading, itemWidth, subtitle = true, title = !options.hideTitle),
            ) {
                items(reading.items.orEmpty(), key = { it.series?.id ?: it.item.id }) {
                    ContentCard(it.item, openContent, card, it.series, it.isNew, openReader, options, download = badges[it.item.id], fitWidth = itemWidth, origin = "continue")
                }
            }
        }

        val updated = vm.updated
        RowError(updated, vm::refresh)
        if (!updated.items.isNullOrEmpty()) {
            ScrollRow(stringResource(R.string.home_updated), onSeeAll = { browse(ContentSort.RECENTLY_UPDATED) }) {
                items(updated.items, key = { it.id }) {
                    ContentCard(
                        it,
                        openContent,
                        card,
                        it.continueInfo?.series,
                        it.continueInfo?.isNew == true,
                        openReader,
                        options,
                        download = badges[it.id],
                        fitWidth = itemWidth,
                        origin = "updated",
                    )
                }
            }
        }

        val added = vm.added
        RowError(added, vm::refresh)
        if (added.loading || !added.items.isNullOrEmpty()) {
            ScrollRow(
                stringResource(R.string.home_added),
                onSeeAll = { browse(ContentSort.CREATED_AT) },
                placeholder = skeletons(added.loading, itemWidth, subtitle = false, title = !options.hideTitle),
            ) {
                items(added.items.orEmpty(), key = { it.id }) { ContentCard(it, openContent, card, options = options, download = badges[it.id], fitWidth = itemWidth, origin = "added") }
            }
        } else if (added.items != null) {
            Text(
                stringResource(R.string.home_nothing_added),
                Modifier.fillMaxWidth().padding(horizontal = pageGutter(), vertical = 48.dp),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = TextAlign.Center,
            )
        }
    }
}

@Composable
private fun RowError(row: HomeRow<*>, retry: () -> Unit) {
    QueryError(row.error, Modifier.padding(horizontal = pageGutter()), retry)
}

/**
 * A row's placeholder while it loads, else null: as many cards as the row shows (8 at most). One
 * node, so TalkBack reads "Loading" once.
 */
private fun skeletons(loading: Boolean, width: Dp, subtitle: Boolean, title: Boolean): (@Composable () -> Unit)? = if (!loading) null else ({
    val label = stringResource(R.string.loading)
    BoxWithConstraints(Modifier.clipToBounds().padding(horizontal = pageGutter()).clearAndSetSemantics { contentDescription = label }) {
        val count = ceil(maxWidth / (width + ScrollRowGap)).toInt().coerceIn(1, 8)
        Row(Modifier.wrapContentWidth(Alignment.Start, unbounded = true), Arrangement.spacedBy(ScrollRowGap)) {
            repeat(count) { ContentCardSkeleton(Modifier.width(width), subtitle, title) }
        }
    }
})

@Composable
private fun NoLibraries(admin: Boolean) {
    Column(
        Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 96.dp),
        Arrangement.spacedBy(16.dp),
        Alignment.CenterHorizontally,
    ) {
        Text(
            stringResource(R.string.home_no_libraries),
            Modifier.semantics { heading() },
            style = MaterialTheme.typography.headlineMedium,
            textAlign = TextAlign.Center,
        )
        Text(
            stringResource(if (admin) R.string.home_no_libraries_admin else R.string.home_no_libraries_user),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
    }
}
