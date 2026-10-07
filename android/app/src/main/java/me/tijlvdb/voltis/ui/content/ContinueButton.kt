package me.tijlvdb.voltis.ui.content

import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContinueTarget
import me.tijlvdb.voltis.domain.catalog.ContinueLabel
import me.tijlvdb.voltis.domain.catalog.continueLabel
import me.tijlvdb.voltis.ui.itemLabels
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VIcons

/**
 * The port of `ContinueReadingButton.vue`: opens what [next] says reading continues with, in the
 * reader ([onRead]) or, for an earlier unread volume, on its page ([onOpen]). Disabled while
 * [next] loads. Only for comics: books can't be read yet. [content] names a held item by its
 * number.
 */
@Composable
fun ContinueButton(
    next: ContinueTarget?,
    failed: Boolean,
    enabled: Boolean,
    onRetry: () -> Unit,
    onRead: (contentId: String) -> Unit,
    onOpen: (contentId: String) -> Unit,
    onReadAgain: () -> Unit,
    modifier: Modifier = Modifier,
    content: Content? = null,
) {
    if (failed) {
        VButton(continueText(next, true, content), onRetry, modifier, enabled = enabled, icon = VIcons.Refresh, danger = true)
        return
    }
    val label = rememberContinueLabel(next, content)
    val text = continueText(next, false, content)
    VButton(
        text,
        onClick = {
            val target = next?.target
            val earlier = next?.earlierUnreadId
            when {
                target != null -> onRead(target.id)
                label == ContinueLabel.Earlier && earlier != null -> onOpen(earlier)
                label == ContinueLabel.Again -> onReadAgain()
            }
        },
        modifier,
        enabled = enabled && label != null && label != ContinueLabel.Empty,
        icon = VIcons.BookOpen,
    )
}

@Composable
private fun rememberContinueLabel(next: ContinueTarget?, content: Content?): ContinueLabel? {
    val labels = itemLabels()
    return remember(next, content, labels) { next?.let { continueLabel(it, content, labels) } }
}

/** What [ContinueButton] shows: also its name while it is disabled for a reason. */
@Composable
fun continueText(next: ContinueTarget?, failed: Boolean, content: Content?): String {
    if (failed) return stringResource(R.string.continue_failed)
    return when (val label = rememberContinueLabel(next, content)) {
        null, ContinueLabel.Continue -> stringResource(R.string.home_continue)
        ContinueLabel.Start -> stringResource(R.string.continue_start)
        ContinueLabel.Next -> stringResource(R.string.reader_read_next)
        is ContinueLabel.Resume -> stringResource(R.string.continue_resume, label.item)
        ContinueLabel.Earlier -> stringResource(R.string.continue_earlier)
        ContinueLabel.Again -> stringResource(R.string.continue_again)
        ContinueLabel.Empty -> stringResource(R.string.continue_empty_chapters)
    }
}
