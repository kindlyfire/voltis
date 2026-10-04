package me.tijlvdb.voltis

import android.app.Activity
import android.content.Intent
import android.os.Bundle

/**
 * Receives `voltis://auth/callback` from the browser and hands it to [MainActivity]. CLEAR_TOP
 * closes the Custom Tab above it, and SINGLE_TOP reuses it via onNewIntent.
 */
class AuthRedirectActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        startActivity(
            Intent(this, MainActivity::class.java)
                .setData(intent.data)
                .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP),
        )
        finish()
    }
}
