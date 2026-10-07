package me.tijlvdb.voltis.ui.lists

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.ListVisibility
import me.tijlvdb.voltis.data.lists.ListView
import me.tijlvdb.voltis.domain.catalog.formatTimestampDate

@Composable
fun visibilityLabel(visibility: String) = when (visibility) {
    ListVisibility.PUBLIC -> stringResource(R.string.list_visibility_public)
    ListVisibility.UNLISTED -> stringResource(R.string.list_visibility_unlisted)
    ListVisibility.PRIVATE -> stringResource(R.string.list_visibility_private)
    else -> visibility
}

@Composable
fun entriesLabel(list: ListView) = pluralStringResource(R.plurals.lists_entries, list.entryCount, list.entryCount)

@Composable
fun updatedLabel(list: ListView) = stringResource(R.string.lists_updated, formatTimestampDate(list.updatedAt))
