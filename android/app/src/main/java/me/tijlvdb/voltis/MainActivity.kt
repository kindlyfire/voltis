package me.tijlvdb.voltis

import android.app.UiModeManager
import android.content.Intent
import android.graphics.Color
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject
import me.tijlvdb.voltis.data.auth.AuthCallbacks
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.content.ContinueResolver
import me.tijlvdb.voltis.data.downloads.StoredCovers
import me.tijlvdb.voltis.data.settings.DeviceSettings
import me.tijlvdb.voltis.data.settings.ThemeMode
import me.tijlvdb.voltis.ui.LocalStoredCovers
import me.tijlvdb.voltis.ui.nav.AppNav
import me.tijlvdb.voltis.ui.nav.LaunchTargets
import me.tijlvdb.voltis.ui.nav.launchTargetOf
import me.tijlvdb.voltis.ui.theme.VoltisTheme

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    @Inject
    lateinit var callbacks: AuthCallbacks

    @Inject
    lateinit var store: SessionStore

    @Inject
    lateinit var settings: DeviceSettings

    @Inject
    lateinit var storedCovers: StoredCovers

    @Inject
    lateinit var targets: LaunchTargets

    @Inject
    lateinit var continueResolver: ContinueResolver

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // The launcher icon over a task a shortcut started (its root intent has another action) would stack a
        // second activity on the first: leave the task as it was instead.
        if (!isTaskRoot && intent.action == Intent.ACTION_MAIN && intent.hasCategory(Intent.CATEGORY_LAUNCHER)) {
            finish()
            return
        }
        enableEdgeToEdge()
        // A recreated activity gets its old intent again; only a fresh start carries a new callback.
        if (savedInstanceState == null) {
            callbacks.handle(intent.data)
            targets.set(launchTargetOf(intent.action, intent.flags))
        }
        setContent {
            // Read in the composition made when the view attaches, so the window's insets are known before the
            // first layout. Read first by a later composition, they arrive one measure late, and that measure
            // clamps a scroll offset restored near a page's end.
            WindowInsets.safeDrawing
            // Until the stored theme is read, only the window background shows.
            val mode = settings.theme.collectAsStateWithLifecycle(null).value ?: return@setContent
            // The window a cold start shows before any of this runs follows the app's night mode, which the system keeps.
            LaunchedEffect(mode) {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                    getSystemService(UiModeManager::class.java).setApplicationNightMode(
                        when (mode) {
                            ThemeMode.System -> UiModeManager.MODE_NIGHT_AUTO
                            ThemeMode.Light -> UiModeManager.MODE_NIGHT_NO
                            ThemeMode.Dark -> UiModeManager.MODE_NIGHT_YES
                        },
                    )
                }
            }
            val dark = when (mode) {
                ThemeMode.System -> isSystemInDarkTheme()
                ThemeMode.Light -> false
                ThemeMode.Dark -> true
            }
            // The system bars' icons follow the app's theme, not the system's.
            LaunchedEffect(dark) {
                val style = if (dark) SystemBarStyle.dark(Color.TRANSPARENT) else SystemBarStyle.light(Color.TRANSPARENT, Color.TRANSPARENT)
                enableEdgeToEdge(style, style)
            }
            val covers by storedCovers.offline.collectAsStateWithLifecycle()
            VoltisTheme(dark) {
                CompositionLocalProvider(LocalStoredCovers provides covers) { AppNav(store.state, targets, continueResolver::resolve) }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        callbacks.handle(intent.data)
        request(intent)
    }

    /** A shortcut's or the download notification's screen, shown once signed in. */
    private fun request(intent: Intent) {
        launchTargetOf(intent.action, intent.flags)?.let(targets::offer)
    }
}
