package me.tijlvdb.voltis.ui.content

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.IntrinsicSize
import kotlin.random.Random
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.lists.entryItem
import me.tijlvdb.voltis.domain.catalog.isSeries
import me.tijlvdb.voltis.ui.kit.VIconButtonStyle
import me.tijlvdb.voltis.ui.kit.VOverflowMenu
import me.tijlvdb.voltis.ui.kit.VSelect
import me.tijlvdb.voltis.ui.kit.VStarToggle
import me.tijlvdb.voltis.ui.statusLabels

/**
 * The status select (`ReadingStatusButton.vue`), the star and the overflow menu, as the web's row
 * under the continue button. [wide] is the web's `sm:w-48` (192 dp) select beside the continue
 * button, growing for a longer status.
 */
@Composable
fun StatusRow(content: Content, vm: ContentViewModel, wide: Boolean = false) {
    Row(if (wide) Modifier else Modifier.fillMaxWidth(), Arrangement.spacedBy(4.dp), Alignment.CenterVertically) {
        // The clear button removes the status and keeps the saved position: it is not ReadingCommands.clear().
        VSelect(
            stringResource(R.string.status_label),
            statusLabels(),
            content.userData?.status,
            vm::setStatus,
            // The field fills what it's given: at least 192 dp, else its value's width.
            if (wide) Modifier.widthIn(min = 192.dp).width(IntrinsicSize.Max) else Modifier.weight(1f),
            clearLabel = stringResource(R.string.status_clear),
            onClear = { vm.setStatus(null) },
            placeholder = stringResource(R.string.status_placeholder),
            enabled = !vm.busy && vm.ready,
            compact = true,
        )
        // Stored on the device and sent later, so it works offline and doesn't wait for a write.
        val starred = content.userData?.starred == true
        VStarToggle(stringResource(R.string.content_starred), starred, vm::setStarred, tonal = true, enabled = vm.ready)
        // A series updates its volumes' progress; an item only has its own to clear.
        val series = content.isSeries
        // Hidden without the `uri` an entry needs (P4 decision 28).
        val item = content.entryItem()
        VOverflowMenu(
            listOfNotNull(
                stringResource(if (series) R.string.content_update_progress else R.string.content_clear) to
                    { vm.show(if (series) ContentDialog.UpdateProgress else ContentDialog.Clear()) },
                // The sheet is one of the page's dialogs, so it outlives the layout switching branches.
                item?.let { stringResource(R.string.lists_add_to_list) to { vm.show(ContentDialog.Lists(Random.nextLong())) } },
            ),
            VIconButtonStyle.Tonal,
            enabled = !vm.busy && vm.ready,
        )
    }
}
