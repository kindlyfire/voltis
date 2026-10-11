package me.tijlvdb.voltis.ui.reader

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.BottomSheetDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
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
import me.tijlvdb.voltis.ui.isLarge
import me.tijlvdb.voltis.ui.kit.AnchorFocusReturn
import me.tijlvdb.voltis.ui.kit.PopoverAnchor
import me.tijlvdb.voltis.ui.kit.VSettingChoice
import me.tijlvdb.voltis.ui.kit.VSheet
import me.tijlvdb.voltis.ui.kit.VSwitchRow

/** The "Display" section of `ReaderSidebar.vue`, with its conditions. Every change is a placement. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ReaderSettingsSheet(vm: ReaderViewModel, viewport: IntSize, onDismiss: () -> Unit, anchor: PopoverAnchor? = null) {
    // Outside the sheet's own window: the anchor is in the reader's.
    AnchorFocusReturn(anchor)
    VSheet(stringResource(R.string.reader_display), onDismiss, width = if (isLarge()) 880.dp else BottomSheetDefaults.SheetMaxWidth) {
        ReaderSettingsRows(vm, viewport)
    }
}

/**
 * The rows of the Display settings: one column, two on a large window. [viewport] is the page area
 * the reader measured, which Auto spread resolves on.
 */
@Composable
fun ReaderSettingsRows(vm: ReaderViewModel, viewport: IntSize) {
    val settings = vm.settings
    val longstrip = vm.mode == ReaderMode.Longstrip
    val auto = stringResource(R.string.reader_auto)
    val status: @Composable ColumnScope.() -> Unit = {
        val view by vm.sync.view.collectAsState()
        ReaderStatusRow(view, vm.statusBusy, vm::runStatus)
    }
    val mode: @Composable ColumnScope.() -> Unit = {
        val paged = stringResource(R.string.reader_mode_paged)
        val strip = stringResource(R.string.reader_mode_longstrip)
        VSettingChoice(
            stringResource(R.string.reader_mode),
            listOf(ReaderMode.Paged to paged, ReaderMode.Longstrip to strip, null to auto),
            vm.seriesSettings.mode,
            vm::setMode,
            auto = (if (vm.autoMode == ReaderMode.Longstrip) strip else paged).takeIf { vm.seriesSettings.mode == null },
        )
    }
    // The longstrip's sliders or the paged direction.
    val flow: @Composable ColumnScope.() -> Unit = {
        if (longstrip) {
            WidthSlider(settings.longstripWidth) { width -> vm.changeSettings { it.copy(longstripWidth = width) } }
            WidthSlider(settings.longstripTapScroll, stringResource(R.string.reader_tap_scroll)) { amount ->
                vm.changeSettings(places = false) { it.copy(longstripTapScroll = amount) }
            }
        } else {
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
        }
    }
    val page: @Composable ColumnScope.() -> Unit = {
        if (!longstrip) {
            FitChoice(settings.fit) { fit -> vm.changeSettings { it.copy(fit = fit) } }
            if (settings.fit == Fit.Screen) {
                VSwitchRow(stringResource(R.string.reader_zoom_wide), settings.zoomWide, { on ->
                    vm.changeSettings { it.copy(zoomWide = on) }
                })
            }
            SpreadChoice(settings.spread, viewport) { spread -> vm.changeSettings { it.copy(spread = spread) } }
            if (settings.spread != SpreadSetting.Single) {
                VSwitchRow(stringResource(R.string.reader_shift_spreads), vm.shifted, { vm.toggleShift() })
            }
        }
    }
    val spaced = Arrangement.spacedBy(16.dp)
    Column(verticalArrangement = spaced) {
        status()
        if (isLarge()) {
            Row(Modifier.fillMaxWidth(), Arrangement.spacedBy(32.dp)) {
                Column(Modifier.weight(1f), spaced) {
                    mode()
                    if (!longstrip) flow()
                }
                Column(Modifier.weight(1f), spaced) {
                    if (longstrip) flow()
                    page()
                }
            }
        } else {
            mode()
            flow()
            page()
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

/** 10 to 100% in steps of 5. */
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
