package me.tijlvdb.voltis.domain.catalog

import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContinueReason
import me.tijlvdb.voltis.data.api.ContinueTarget

/** What the continue button says and does: the port of `ContinueReadingButton.vue`. */
sealed interface ContinueLabel {
    data object Start : ContinueLabel
    data object Continue : ContinueLabel
    data object Next : ContinueLabel

    /** "Resume Volume 3": a held item, named within the series. */
    data class Resume(val item: String) : ContinueLabel

    /** Opens the earlier volume's page rather than the reader. */
    data object Earlier : ContinueLabel

    /** Clears the reading state and starts over. */
    data object Again : ContinueLabel

    /** Nothing to read: the button is disabled. */
    data object Empty : ContinueLabel
}

/** [series] is the page's content, which names a held item by its number. */
fun continueLabel(target: ContinueTarget, series: Content?, labels: ItemLabels): ContinueLabel = when {
    target.reason == ContinueReason.EMPTY -> ContinueLabel.Empty
    target.reason == ContinueReason.HELD && target.target != null -> ContinueLabel.Resume(itemName(target.target, series, labels))
    target.reason == ContinueReason.EARLIER_UNREAD -> ContinueLabel.Earlier
    target.reason == ContinueReason.COMPLETED || target.reason == ContinueReason.CAUGHT_UP -> ContinueLabel.Again
    target.action == "resume" -> ContinueLabel.Continue
    target.action == "next" -> ContinueLabel.Next
    else -> ContinueLabel.Start
}
