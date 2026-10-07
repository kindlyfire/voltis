package me.tijlvdb.voltis.ui.libraries

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Badge
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.LibraryVisibility
import me.tijlvdb.voltis.ui.LoadStatus
import me.tijlvdb.voltis.ui.RefreshOnResume
import me.tijlvdb.voltis.ui.ServerOffline
import me.tijlvdb.voltis.ui.isOnline
import me.tijlvdb.voltis.ui.bottomSpace
import me.tijlvdb.voltis.ui.kit.PageHeader
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.pageGutter
import me.tijlvdb.voltis.ui.readableWidth
import me.tijlvdb.voltis.ui.topInset

/**
 * Downloads, Lists and Discover under "Collections"; then under "Libraries" All libraries, the shown ones, and the
 * overflow ones under "Others". Hidden ones are left out.
 */
@Composable
fun LibrariesScreen(
    openLibrary: (libraryId: String) -> Unit,
    browse: () -> Unit,
    discover: () -> Unit,
    downloads: () -> Unit,
    lists: () -> Unit,
    vm: LibrariesViewModel = hiltViewModel(),
) {
    RefreshOnResume(vm.refresher)
    val me by vm.me.collectAsStateWithLifecycle()
    val active by vm.activeDownloads.collectAsStateWithLifecycle(0)
    val attention by vm.attention.collectAsStateWithLifecycle(0)
    Surface(Modifier.fillMaxSize()) {
        Column(Modifier.topInset().verticalScroll(rememberScrollState()).readableWidth(pageGutter()).padding(bottom = bottomSpace())) {
            PageHeader(stringResource(R.string.tab_libraries), Modifier.padding(horizontal = pageGutter()).padding(bottom = 8.dp))
            // Above the libraries' load, so these show while it runs or fails.
            Heading(stringResource(R.string.libraries_collections), top = 8.dp)
            NavRow(VIcons.Download, stringResource(R.string.downloads_title), onClick = downloads, trailing = {
                if (active > 0) {
                    Text(
                        pluralStringResource(R.plurals.downloads_active, active, active),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        style = MaterialTheme.typography.bodyMedium,
                    )
                }
                if (attention > 0) {
                    val label = pluralStringResource(R.plurals.attention_items, attention, attention)
                    Badge(Modifier.clearAndSetSemantics { contentDescription = label }) { Text(attention.toString()) }
                }
            })
            NavRow(VIcons.ListAlt, stringResource(R.string.lists_title), onClick = lists)
            NavRow(VIcons.Explore, stringResource(R.string.discover), onClick = discover)
            Heading(stringResource(R.string.libraries_section))
            // Downloads is the row above.
            if (!isOnline()) return@Column ServerOffline(downloads = false)
            Box(Modifier.padding(horizontal = pageGutter())) { LoadStatus(vm.libraries == null, vm.error, vm::load) }
            val libraries = vm.libraries ?: return@Column
            if (libraries.isEmpty()) {
                Text(
                    stringResource(R.string.home_no_libraries),
                    Modifier.padding(horizontal = pageGutter()),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                return@Column
            }
            val visibility = libraries.groupBy { me?.prefs?.libraryVisibility(it.id) ?: LibraryVisibility.SHOW }
            NavRow(VIcons.Bookshelf, stringResource(R.string.libraries_all), onClick = browse)
            for (library in visibility[LibraryVisibility.SHOW].orEmpty()) NavRow(VIcons.Bookshelf, library.name) { openLibrary(library.id) }
            val others = visibility[LibraryVisibility.OVERFLOW].orEmpty()
            if (others.isNotEmpty()) {
                Heading(stringResource(R.string.libraries_others))
                for (library in others) NavRow(VIcons.Bookshelf, library.name) { openLibrary(library.id) }
            }
        }
    }
}

@Composable
private fun Heading(text: String, top: Dp = 20.dp) {
    Text(
        text,
        Modifier.padding(horizontal = pageGutter()).padding(top = top, bottom = 4.dp).semantics { heading() },
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        style = MaterialTheme.typography.titleSmall,
    )
}

/** [trailing] is read with the row's name. */
@Composable
private fun NavRow(icon: Painter, name: String, trailing: @Composable () -> Unit = {}, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(role = Role.Button, onClick = onClick).heightIn(min = 56.dp).padding(horizontal = pageGutter()),
        Arrangement.spacedBy(16.dp),
        Alignment.CenterVertically,
    ) {
        Icon(icon, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(name, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
        trailing()
    }
}
