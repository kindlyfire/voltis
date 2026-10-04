package me.tijlvdb.voltis.data.api

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import java.io.IOException
import okhttp3.HttpUrl

/** A failed request to a LAN address without the permission Android 17 requires for it. */
class LocalNetworkException : IOException()

/** True when [url] is on the local network and Android (17+) may block it until the user allows it. */
fun Context.lacksLocalNetwork(url: HttpUrl): Boolean =
    Build.VERSION.SDK_INT >= Build.VERSION_CODES.CINNAMON_BUN &&
        ServerUrl.needsLocalNetwork(url.host) &&
        checkSelfPermission(Manifest.permission.ACCESS_LOCAL_NETWORK) != PackageManager.PERMISSION_GRANTED
