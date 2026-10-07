package me.tijlvdb.voltis.ui.grid

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.data.reading.BulkState

/**
 * One screen's batch: followed from its start until the runner shows another value. Reconciled
 * whenever either side changes, from their current values, not from what the flows delivered.
 * Everything runs on Main, the view model's dispatcher.
 */
class BatchFollower(scope: CoroutineScope, private val runner: StateFlow<BulkState?>, private val onEnd: () -> Unit) {
    private val mine = MutableStateFlow<BulkState?>(null) // as started; null when not following
    private val _state = MutableStateFlow<BulkState?>(null)

    /** This screen's batch's progress while followed. */
    val state: StateFlow<BulkState?> = _state.asStateFlow()

    init {
        scope.launch {
            // Only a wake-up: what is acted on is read below.
            combine(mine, runner) { _, _ -> }.collect {
                val m = mine.value ?: return@collect
                val s = runner.value
                if (s?.id == m.id) {
                    _state.value = s
                    return@collect
                }
                // Any other runner value (null, or a later batch) means it is over, even if no non-null value was delivered.
                mine.value = null
                _state.value = null
                onEnd()
            }
        }
    }

    fun follow(started: BulkState) {
        _state.value = started
        mine.value = started
    }

    /** Hide: this screen no longer follows its batch, so a later end can't touch it. */
    fun forget() {
        mine.value = null
        _state.value = null
    }
}
