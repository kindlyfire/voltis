package me.tijlvdb.voltis.ui

import android.content.ActivityNotFoundException
import android.content.Context
import androidx.browser.customtabs.CustomTabsIntent
import androidx.core.net.toUri

/**
 * Opens [url] in a Custom Tab in this task (no NEW_TASK), so the auth callback's CLEAR_TOP closes
 * it. Browsers without Custom Tabs handle the same ACTION_VIEW intent. False if there's no browser.
 */
fun Context.openInBrowser(url: String): Boolean = try {
    CustomTabsIntent.Builder().build().launchUrl(this, url.toUri())
    true
} catch (_: ActivityNotFoundException) {
    false
}
