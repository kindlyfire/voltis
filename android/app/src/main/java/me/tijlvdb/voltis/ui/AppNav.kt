package me.tijlvdb.voltis.ui

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import kotlinx.coroutines.flow.StateFlow
import kotlinx.serialization.Serializable
import me.tijlvdb.voltis.data.auth.SessionState
import me.tijlvdb.voltis.ui.home.HomeScreen
import me.tijlvdb.voltis.ui.login.LoginScreen
import me.tijlvdb.voltis.ui.server.ServerScreen

@Serializable
data object ServerRoute

@Serializable
data object LoginRoute

@Serializable
data object HomeRoute

/**
 * The session state picks the screen; screens change it rather than navigating themselves.
 */
@Composable
fun AppNav(session: StateFlow<SessionState>) {
    val state by session.collectAsStateWithLifecycle()
    val start: Any = when (state) {
        SessionState.Loading -> {
            Surface(Modifier.fillMaxSize()) {}
            return
        }
        SessionState.NoServer -> ServerRoute
        is SessionState.SignedOut, is SessionState.NeedsReauth -> LoginRoute
        is SessionState.SignedIn -> HomeRoute
    }
    // key() changes the saveable key, so a back stack saved under another start isn't restored.
    key(start) {
        NavHost(rememberNavController(), startDestination = start) {
            composable<ServerRoute> { ServerScreen() }
            composable<LoginRoute> { LoginScreen() }
            composable<HomeRoute> { HomeScreen() }
        }
    }
}
