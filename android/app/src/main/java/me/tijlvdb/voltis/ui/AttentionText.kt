package me.tijlvdb.voltis.ui

import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.domain.reading.SyncCommand
import me.tijlvdb.voltis.domain.sync.NoticeDetail
import me.tijlvdb.voltis.domain.sync.NoticeKind
import me.tijlvdb.voltis.ui.UiText

/** A dropped command, as a notice names it. */
fun commandLabel(command: String?) = UiText.Res(
    when (command) {
        SyncCommand.CLEAR -> R.string.notice_cmd_clear
        SyncCommand.MARK_COMPLETED -> R.string.notice_cmd_mark_completed
        SyncCommand.MARK_THROUGH -> R.string.notice_cmd_mark_through
        SyncCommand.MARK_SERIES_COMPLETED -> R.string.notice_cmd_series_completed
        SyncCommand.UNDO -> R.string.notice_cmd_undo
        else -> R.string.notice_cmd_other
    },
)

/** "Mark completed wasn't applied: changed on another device", for a command dropped for a change elsewhere. */
fun notAppliedText(command: String?) = UiText.Format(R.string.notice_not_applied, listOf(commandLabel(command)))

/** A stored notice's sentence (P2 §11). A kind this version doesn't know shows its message. */
fun noticeText(kind: String, detail: NoticeDetail): UiText = when (kind) {
    NoticeKind.CHANGED_ELSEWHERE -> if (detail.uncertain) UiText.Format(R.string.notice_maybe_not_applied, listOf(commandLabel(detail.command))) else notAppliedText(detail.command)
    NoticeKind.REFUSED -> UiText.Format(R.string.notice_refused, listOf(detail.message.orEmpty()))
    NoticeKind.GONE -> UiText.Res(R.string.notice_gone)
    else -> UiText.Raw(detail.message.orEmpty())
}
