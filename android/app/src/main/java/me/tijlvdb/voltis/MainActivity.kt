package me.tijlvdb.voltis

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject
import me.tijlvdb.voltis.data.auth.AuthCallbacks
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.ui.AppNav
import me.tijlvdb.voltis.ui.theme.VoltisTheme

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    @Inject
    lateinit var callbacks: AuthCallbacks

    @Inject
    lateinit var store: SessionStore

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        // A recreated activity gets its old intent again; only a fresh start carries a new callback.
        if (savedInstanceState == null) callbacks.handle(intent.data)
        setContent { VoltisTheme { AppNav(store.state) } }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        callbacks.handle(intent.data)
    }
}
