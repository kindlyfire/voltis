package me.tijlvdb.voltis.ui.settings

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.res.stringResource
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.settings.DeviceSettings
import me.tijlvdb.voltis.data.settings.ReaderSettingsStore
import me.tijlvdb.voltis.domain.comic.Fit
import me.tijlvdb.voltis.domain.comic.ReaderSettings
import me.tijlvdb.voltis.ui.PushedScreen
import me.tijlvdb.voltis.ui.kit.LocalSnackbars
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VSwitchRow
import me.tijlvdb.voltis.ui.reader.FitChoice
import me.tijlvdb.voltis.ui.reader.SpreadChoice
import me.tijlvdb.voltis.ui.reader.WidthSlider

@HiltViewModel
class ReaderDefaultsViewModel @Inject constructor(
    private val store: ReaderSettingsStore,
    private val device: DeviceSettings,
) : ViewModel() {
    /** Null until the stored ones are read. */
    val settings = store.settings.stateIn(viewModelScope, SharingStarted.Eagerly, null)

    fun change(change: (ReaderSettings) -> ReaderSettings) {
        viewModelScope.launch { store.update(change) }
    }

    fun showTutorial() {
        viewModelScope.launch { device.resetReaderTutorial() }
    }
}

/** The reader settings that aren't a series' or a volume's: what the reader's sheet also edits. */
@Composable
fun ReaderDefaultsScreen(onBack: () -> Unit, vm: ReaderDefaultsViewModel = hiltViewModel()) {
    val snackbars = LocalSnackbars.current
    val stored by vm.settings.collectAsStateWithLifecycle()
    PushedScreen(stringResource(R.string.defaults_title), onBack) {
        val settings = stored ?: return@PushedScreen
        FitChoice(settings.fit) { fit -> vm.change { it.copy(fit = fit) } }
        SpreadChoice(settings.spread) { spread -> vm.change { it.copy(spread = spread) } }
        if (settings.fit == Fit.Screen) {
            VSwitchRow(stringResource(R.string.reader_zoom_wide), settings.zoomWide, { on -> vm.change { it.copy(zoomWide = on) } })
        }
        VSwitchRow(stringResource(R.string.defaults_invert_controls), settings.invertRtlControls, { on ->
            vm.change { it.copy(invertRtlControls = on) }
        })
        WidthSlider(settings.longstripWidth, stringResource(R.string.defaults_longstrip_width)) { width ->
            vm.change { it.copy(longstripWidth = width) }
        }
        val reset = stringResource(R.string.defaults_tutorial_reset)
        VButton(
            stringResource(R.string.defaults_show_tutorial),
            {
                vm.showTutorial()
                snackbars.show(reset)
            },
            style = VButtonStyle.Tonal,
        )
    }
}
