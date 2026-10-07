package me.tijlvdb.voltis.ui.reader

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import kotlin.math.roundToInt
import kotlinx.coroutines.delay
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.domain.comic.Extent
import me.tijlvdb.voltis.domain.comic.Fit
import me.tijlvdb.voltis.domain.comic.ReaderMode
import me.tijlvdb.voltis.domain.comic.ReadingDirection
import me.tijlvdb.voltis.domain.comic.SpreadSetting
import me.tijlvdb.voltis.domain.comic.resolveDouble
import me.tijlvdb.voltis.ui.kit.AnchorFocusReturn
import me.tijlvdb.voltis.ui.kit.PopoverAnchor
import me.tijlvdb.voltis.ui.kit.VSettingChoice
import me.tijlvdb.voltis.ui.kit.VSheet
import me.tijlvdb.voltis.ui.kit.VSwitchRow

/** The "Display" section of `ReaderSidebar.vue`, with its conditions. Every change is a placement. */
@Composable
fun ReaderSettingsSheet(vm: ReaderViewModel, viewport: IntSize, onDismiss: () -> Unit, anchor: PopoverAnchor? = null) {
    // Outside the sheet's own window: the anchor is in the reader's.
    AnchorFocusReturn(anchor)
    VSheet(stringResource(R.string.reader_display), onDismiss) {
        ReaderSettingsRows(vm, viewport)
    }
}

/**
 * The rows of the Display settings, in a sheet on a phone and in a side panel on a large window.
 * [viewport] is the page area the reader measured, which Auto spread resolves on.
 */
@Composable
fun ReaderSettingsRows(vm: ReaderViewModel, viewport: IntSize) {
    val settings = vm.settings
    run {
        Column(verticalArrangement = Arrangement.spacedBy(16.dp)) {
            val view by vm.sync.view.collectAsState()
            ReaderStatusRow(view, vm.statusBusy, vm::runStatus)
            val paged = stringResource(R.string.reader_mode_paged)
            val longstrip = stringResource(R.string.reader_mode_longstrip)
            val auto = stringResource(R.string.reader_auto)
            VSettingChoice(
                stringResource(R.string.reader_mode),
                listOf(ReaderMode.Paged to paged, ReaderMode.Longstrip to longstrip, null to auto),
                vm.seriesSettings.mode,
                vm::setMode,
                auto = (if (vm.autoMode == ReaderMode.Longstrip) longstrip else paged).takeIf { vm.seriesSettings.mode == null },
            )
            if (vm.mode == ReaderMode.Longstrip) {
                WidthSlider(settings.longstripWidth) { width -> vm.changeSettings { it.copy(longstripWidth = width) } }
                return@Column
            }
            VSettingChoice(
                stringResource(R.string.reader_direction),
                listOf(
                    ReadingDirection.Ltr to stringResource(R.string.reader_direction_ltr),
                    ReadingDirection.Rtl to stringResource(R.string.reader_direction_rtl),
                    null to auto,
                ),
                vm.seriesSettings.direction,
                vm::setDirection,
                auto = stringResource(
                    if (vm.autoDirection == ReadingDirection.Rtl) R.string.reader_right_to_left else R.string.reader_left_to_right,
                ).takeIf { vm.seriesSettings.direction == null },
            )
            if (vm.direction == ReadingDirection.Rtl) {
                VSwitchRow(stringResource(R.string.reader_invert_controls), settings.invertRtlControls, { on ->
                    vm.changeSettings { it.copy(invertRtlControls = on) }
                })
            }
            FitChoice(settings.fit) { fit -> vm.changeSettings { it.copy(fit = fit) } }
            SpreadChoice(settings.spread, viewport) { spread -> vm.changeSettings { it.copy(spread = spread) } }
            if (settings.spread != SpreadSetting.Single) {
                VSwitchRow(stringResource(R.string.reader_shift_spreads), vm.shifted, { vm.toggleShift() })
            }
            if (settings.fit == Fit.Screen) {
                VSwitchRow(stringResource(R.string.reader_zoom_wide), settings.zoomWide, { on ->
                    vm.changeSettings { it.copy(zoomWide = on) }
                })
            }
        }
    }
}

// The rows below are also the Reader defaults screen's.

@Composable
fun FitChoice(fit: Fit, onSelect: (Fit) -> Unit) {
    VSettingChoice(
        stringResource(R.string.reader_fit),
        listOf(
            Fit.Screen to stringResource(R.string.reader_fit_screen),
            Fit.Width to stringResource(R.string.reader_fit_width),
            Fit.Height to stringResource(R.string.reader_fit_height),
        ),
        fit,
        onSelect,
    )
}

/** Auto resolves on the reader's [viewport], or on the window where there is no reader (the defaults). */
@Composable
fun SpreadChoice(spread: SpreadSetting, viewport: IntSize? = null, onSelect: (SpreadSetting) -> Unit) {
    val single = stringResource(R.string.reader_spread_single)
    val double = stringResource(R.string.reader_spread_double)
    val window = viewport ?: LocalWindowInfo.current.containerSize
    VSettingChoice(
        stringResource(R.string.reader_spread),
        listOf(SpreadSetting.Single to single, SpreadSetting.Double to double, SpreadSetting.Auto to stringResource(R.string.reader_auto)),
        spread,
        onSelect,
        auto = (if (resolveDouble(SpreadSetting.Auto, Extent(window.width.toDouble(), window.height.toDouble()))) double else single)
            .takeIf { spread == SpreadSetting.Auto },
    )
}

/** 10 to 100% in steps of 5. The strip is laid out again at each step. */
@Composable
fun WidthSlider(width: Int, name: String = stringResource(R.string.reader_width), onChange: (Int) -> Unit) {
    // The thumb follows the finger; the stored value follows a moment later.
    var shown by remember { mutableIntStateOf(width) }
    var pending by remember { mutableStateOf<Int?>(null) }
    // One write once the thumb pauses, not one per step.
    LaunchedEffect(pending) {
        val to = pending ?: return@LaunchedEffect
        delay(200)
        pending = null
        onChange(to)
    }
    val value = stringResource(R.string.reader_percent, shown)
    Column {
        Row(Modifier.fillMaxWidth(), Arrangement.SpaceBetween) {
            Text(name, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
            Text(value, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
        }
        Slider(
            value = shown.toFloat(),
            onValueChange = {
                val to = it.roundToInt()
                if (to != shown) {
                    shown = to
                    pending = to
                }
            },
            onValueChangeFinished = {
                pending?.let {
                    pending = null
                    onChange(it)
                }
            },
            modifier = Modifier.semantics {
                contentDescription = name
                stateDescription = value
            },
            valueRange = 10f..100f,
            steps = 17,
        )
    }
}
