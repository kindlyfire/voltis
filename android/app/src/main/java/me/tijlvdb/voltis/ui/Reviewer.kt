package me.tijlvdb.voltis.ui

import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.toUiText
import me.tijlvdb.voltis.domain.reading.ReadingSync
import me.tijlvdb.voltis.domain.reading.ReviewOutcome
import me.tijlvdb.voltis.domain.reading.ReviewSession
import me.tijlvdb.voltis.domain.reading.SyncUnavailable

/** An item under review: [title] names it in the dialog, also once it has left the list. */
data class Review(val contentId: String, val title: String, val session: ReviewSession)

/**
 * One review at a time of an item changed on another device ("Needs attention", P2 §5), for
 * [account]. The engine ends it ([ReviewSession.ended]); so do another review, and [end], which its
 * screen calls when it stops ([EndReviewOnStop]). [onMessage] gets why it couldn't check.
 */
class Reviewer(private val scope: CoroutineScope, private val sync: ReadingSync, private val account: String?, private val onMessage: (UiText) -> Unit) {
    /** Snapshot state. */
    var current by mutableStateOf<Review?>(null)
        private set

    private var watch: Job? = null

    fun start(contentId: String, title: String) {
        end()
        val session = try {
            sync.review(account ?: throw SyncUnavailable.AccountChanged(), contentId)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            return onMessage(e.toUiText())
        }
        current = Review(contentId, title, session)
        watch = scope.launch {
            session.ended.first { it }
            if (session.outcome.value == ReviewOutcome.FAILED) onMessage(UiText.Res(R.string.attention_review_failed))
            end()
        }
    }

    fun end() {
        watch?.cancel()
        watch = null
        current?.session?.detach()
        current = null
    }
}

/** A review ends when its screen stops or leaves: it is never left reading for a screen nobody sees. */
@Composable
fun EndReviewOnStop(reviewer: Reviewer) {
    LifecycleEventEffect(Lifecycle.Event.ON_STOP) { reviewer.end() }
    DisposableEffect(reviewer) { onDispose { reviewer.end() } }
}
