package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

/**
 * A single-choice pill, named [label]: as wide as its labels need (at most the width it is given),
 * or with [block] the full width. Choosing the selected option does nothing. Its segments are
 * equal while every label fits; otherwise each is as wide as its label needs, and the selected one
 * adds its check, as the web's `ASegmented` does. When even that doesn't fit, all the labels
 * shrink to one size, down to 85 %, and then wrap between words. A word is never broken: the size
 * goes below 85 % when the longest words need it.
 */
@Composable
fun <T> VSegmented(
    label: String,
    options: List<Pair<T, String>>,
    selected: T,
    onSelect: (T) -> Unit,
    modifier: Modifier = Modifier,
    block: Boolean = true,
) {
    val scheme = MaterialTheme.colorScheme
    val colors = SegmentedButtonDefaults.colors(
        activeContainerColor = scheme.secondaryContainer,
        activeContentColor = scheme.onSecondaryContainer,
        activeBorderColor = scheme.outline,
        inactiveContainerColor = Color.Transparent,
        inactiveContentColor = scheme.onSurface,
        inactiveBorderColor = scheme.outline,
    )
    val style = MaterialTheme.typography.labelLarge
    val measurer = rememberTextMeasurer()
    val density = LocalDensity.current
    val width = { text: String -> measurer.measure(text, style, maxLines = 1, softWrap = false).size.width.toFloat() }
    val labels = remember(options, style, density, measurer) { options.map { width(it.second) } }
    val words = remember(options, style, density, measurer) { options.map { (_, text) -> text.split(' ').maxOf(width) } }
    BoxWithConstraints(if (block) modifier.fillMaxWidth() else modifier) {
        // Each segment's padding and border; the check and its gap show on the selected one only.
        val chrome = with(density) { (Padding * 2 + 1.dp).toPx() }
        val check = with(density) { (SegmentedButtonDefaults.IconSize + 8.dp).toPx() }
        // A px of slack per segment, so rounding never cuts a label.
        val natural = labels.sum() + (chrome + 1) * options.size + check
        val room = if (block) constraints.maxWidth.toFloat() else minOf(constraints.maxWidth.toFloat(), natural)
        val fit = (room - chrome * options.size - check) / labels.sum()
        val wrap = fit < MinScale
        val scale = if (wrap) minOf(MinScale, (room - chrome * options.size - check) / words.sum()) else minOf(fit, 1f)
        // Wrapping, a segment needs room for its longest word only.
        val needs = if (wrap) words else labels
        val equal = labels.all { it + chrome + check <= room / options.size }
        // The size in px, converted back: sp grow non-linearly with the font scale.
        val fontSize = with(density) { (style.fontSize.toPx() * scale).toSp() }
        // As tall as the tallest segment, so a wrapped label leaves none of them shorter.
        val extent = if (block) Modifier.fillMaxWidth() else Modifier.width(with(density) { room.toDp() })
        SingleChoiceSegmentedButtonRow(extent.height(IntrinsicSize.Min).semantics { contentDescription = label }) {
            options.forEachIndexed { index, (value, text) ->
                val on = value == selected
                SegmentedButton(
                    selected = on,
                    onClick = { if (!on) onSelect(value) },
                    shape = SegmentedButtonDefaults.itemShape(index, options.size),
                    // Overrides the button's own equal weight.
                    modifier = Modifier.weight(if (equal) 1f else needs[index] * scale + chrome + if (on) check else 0f).fillMaxHeight(),
                    colors = colors,
                    contentPadding = PaddingValues(horizontal = Padding),
                ) {
                    Text(text, fontSize = fontSize, textAlign = TextAlign.Center, maxLines = if (wrap) 2 else 1, softWrap = wrap, overflow = TextOverflow.Ellipsis)
                }
            }
        }
    }
}

private val Padding = 12.dp
private const val MinScale = 0.85f
