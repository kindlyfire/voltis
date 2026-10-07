package me.tijlvdb.voltis.ui.content

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalView
import kotlinx.coroutines.delay
import me.tijlvdb.voltis.ui.kit.PopoverAnchor
import me.tijlvdb.voltis.ui.kit.focusForTalkBack
import me.tijlvdb.voltis.ui.kit.popoverAnchor
import me.tijlvdb.voltis.ui.rememberTouchExploration
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.theme.VoltisTheme

/**
 * The port of `RatingButton.vue`: five stars as a radio group named "Your rating", then a clear
 * button, shown while there is a rating. [onRate] gets 1 to 5, or null. Disabled, the stars are dimmed. [centred] false puts the
 * stars at the start.
 */
@Composable
fun RatingRow(rating: Int?, onRate: (Int?) -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true, centred: Boolean) {
    val group = stringResource(R.string.rating_label)
    // Clearing removes the control that has focus: the first star takes it, for the keyboard and for TalkBack. Only after the
    // user's own clear, and only once the null rating has arrived (the write is async): the request stays pending until then. It
    // is dropped if another rating arrives instead, after [PendingMs] (a failed clear), or on another action of the user's.
    // Without a pending request, a rating changing from elsewhere never moves focus.
    var clearing by remember { mutableStateOf<Int?>(null) }
    val firstStar = remember { FocusRequester() }
    val anchor = remember { PopoverAnchor() }
    val view = LocalView.current
    val explore by rememberTouchExploration()
    val current by rememberUpdatedState(rating)
    LaunchedEffect(clearing) {
        if (clearing == null) return@LaunchedEffect
        delay(PendingMs)
        // Only while still waiting for the null rating: a handoff already under way finishes.
        if (current != null) clearing = null
    }
    LaunchedEffect(clearing, rating) {
        val from = clearing ?: return@LaunchedEffect
        when {
            rating == null -> {
                // The stars are laid out: they were there before, and this frame has the clear button gone.
                withFrameNanos { }
                runCatching { firstStar.requestFocus() }
                if (explore) {
                    delay(FocusDelayMs)
                    focusForTalkBack(view, anchor)
                }
                // Consumed last: `clearing` keys this effect, so clearing it earlier would cancel the delay above.
                clearing = null
            }
            rating != from -> clearing = null
        }
    }
    Layout(
        content = {
            Row(Modifier.selectableGroup().alpha(if (enabled) 1f else 0.38f).semantics { contentDescription = group }) {
                for (star in 1..5) {
                    val filled = star <= (rating ?: 0)
                    val name = pluralStringResource(R.plurals.rating_stars, star, star)
                    Box(
                        Modifier
                            .then(if (star == 1) Modifier.focusRequester(firstStar).popoverAnchor(anchor) else Modifier)
                            .size(StarSize)
                            .clip(CircleShape)
                            .selectable(star == rating, enabled, Role.RadioButton) { clearing = null; onRate(star) }
                            .semantics { contentDescription = name },
                        Alignment.Center,
                    ) {
                        Icon(
                            if (filled) VIcons.StarFilled else VIcons.Star,
                            contentDescription = null,
                            Modifier.size(22.dp),
                            tint = if (filled) VoltisTheme.colors.star else MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
            // Only while there is a rating to remove.
            // A star's size, like the stars: a larger target would overhang the row into what is above it.
            if (rating != null) {
                Box(
                    Modifier.size(StarSize).clip(CircleShape)
                        .clickable(enabled, role = Role.Button) { clearing = rating; onRate(null) },
                    Alignment.Center,
                ) {
                    Icon(VIcons.Close, stringResource(R.string.rating_clear), Modifier.size(20.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        },
        modifier = modifier.fillMaxWidth(),
    ) { measurables, constraints ->
        val loose = constraints.copy(minWidth = 0)
        val starsAt = measurables[0].measure(loose)
        val clearAt = measurables.getOrNull(1)?.measure(loose)
        val clearWidth = clearAt?.width ?: 0
        // The stars are centred where the clear button still fits beside them, else as near as it allows.
        val x = if (!centred) 0 else minOf((constraints.maxWidth - starsAt.width) / 2, constraints.maxWidth - starsAt.width - clearWidth).coerceAtLeast(0)
        layout(constraints.maxWidth, maxOf(starsAt.height, clearAt?.height ?: 0)) {
            starsAt.place(x, 0)
            clearAt?.place(x + starsAt.width, 0)
        }
    }
}

/** TalkBack takes a moment to settle on the stars once the button is gone. */
private const val FocusDelayMs = 300L

/** How long a clear may take to arrive before it is taken for failed. */
private const val PendingMs = 5000L

/** A star's square: compact, below the usual 48 dp, so the row sits close to what is above it. */
private val StarSize = 32.dp
