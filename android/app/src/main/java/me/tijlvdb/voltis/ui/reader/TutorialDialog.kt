package me.tijlvdb.voltis.ui.reader

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.settings.DeviceSettings
import me.tijlvdb.voltis.domain.comic.HEIGHT_ZONE
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.theme.VoltisTheme

@HiltViewModel
class TutorialViewModel @Inject constructor(private val settings: DeviceSettings) : ViewModel() {
    private val dismissed = MutableStateFlow(false)

    /** Null until the stored flag is read. Dismissing counts at once, whether or not the flag could be stored (a full disk). */
    val seen = combine(settings.readerTutorialSeen, dismissed) { stored, dismissed -> stored || dismissed }
        .stateIn(viewModelScope, SharingStarted.Eagerly, null)

    fun dismiss() {
        dismissed.value = true
        viewModelScope.launch { settings.markReaderTutorialSeen() }
    }
}

/** The tutorial, until it was dismissed once on this device. [flipped] swaps the diagram's sides. */
@Composable
fun ReaderTutorial(flipped: Boolean, vm: TutorialViewModel = hiltViewModel()) {
    val seen by vm.seen.collectAsStateWithLifecycle()
    if (seen != false) return
    AlertDialog(
        onDismissRequest = vm::dismiss,
        confirmButton = { VButton(stringResource(R.string.tutorial_done), vm::dismiss) },
        title = { Text(stringResource(R.string.tutorial_title), style = MaterialTheme.typography.titleLarge) },
        containerColor = VoltisTheme.colors.raised,
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), Arrangement.spacedBy(8.dp), Alignment.CenterHorizontally) {
                val colors = MaterialTheme.colorScheme
                val prev = colors.primary.copy(alpha = 0.14f)
                val next = colors.primary.copy(alpha = 0.32f)
                val menu = colors.onSurfaceVariant.copy(alpha = 0.22f)
                ZoneDiagram(prev, menu, next, flipped)
                Row(Modifier.clearAndSetSemantics {}, Arrangement.spacedBy(16.dp)) {
                    Legend(prev, stringResource(R.string.tutorial_previous))
                    Legend(menu, stringResource(R.string.tutorial_menu))
                    Legend(next, stringResource(R.string.tutorial_next))
                }
                for (line in listOf(R.string.tutorial_zones, R.string.tutorial_swipe, R.string.tutorial_zoom, R.string.tutorial_center)) {
                    Text(stringResource(line), Modifier.fillMaxWidth(), color = colors.onSurface)
                }
            }
        },
    )
}

/** The tap zones as `getClickZone` cuts them. Flipped swaps only the side tints: the chevrons still point at the screen edges. */
@Composable
private fun ZoneDiagram(prev: Color, menu: Color, next: Color, flipped: Boolean) {
    val outline = MaterialTheme.colorScheme.outline
    val shape = RoundedCornerShape(8.dp)
    CompositionLocalProvider(LocalLayoutDirection provides LayoutDirection.Ltr) {
        Column(
            Modifier
                .widthIn(max = 256.dp)
                .aspectRatio(3f / 4f)
                .clip(shape)
                .border(1.dp, outline, shape)
                .drawWithContent {
                    drawContent()
                    val dashes = PathEffect.dashPathEffect(floatArrayOf(4.dp.toPx(), 3.dp.toPx()))
                    val top = size.height * HEIGHT_ZONE
                    val bottom = size.height - top
                    for (y in listOf(top, bottom)) drawLine(outline, Offset(0f, y), Offset(size.width, y), 1.dp.toPx(), pathEffect = dashes)
                    for (x in listOf(size.width / 3, size.width * 2 / 3)) {
                        drawLine(outline, Offset(x, top), Offset(x, bottom), 1.dp.toPx(), pathEffect = dashes)
                    }
                }
                .clearAndSetSemantics {},
        ) {
            Zone(VIcons.ChevronUp, prev, Modifier.weight(HEIGHT_ZONE).fillMaxWidth())
            Row(Modifier.weight(1 - 2 * HEIGHT_ZONE)) {
                Zone(VIcons.ChevronLeft, if (flipped) next else prev, Modifier.weight(1f).fillMaxHeight())
                Zone(VIcons.Menu, menu, Modifier.weight(1f).fillMaxHeight())
                Zone(VIcons.ChevronRight, if (flipped) prev else next, Modifier.weight(1f).fillMaxHeight())
            }
            Zone(VIcons.ChevronDown, next, Modifier.weight(HEIGHT_ZONE).fillMaxWidth())
        }
    }
}

@Composable
private fun Zone(icon: Painter, tint: Color, modifier: Modifier) {
    Box(modifier.background(tint), Alignment.Center) { Icon(icon, contentDescription = null) }
}

@Composable
private fun Legend(tint: Color, label: String) {
    Row(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(10.dp).background(tint, RoundedCornerShape(3.dp)))
        Text(label, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
    }
}
