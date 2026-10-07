package me.tijlvdb.voltis.data.net

import android.net.NetworkCapabilities
import android.net.NetworkRequest
import androidx.work.Constraints
import androidx.work.NetworkType

/**
 * Any network with internet, validated or not (a server on a LAN the system hasn't validated),
 * VPNs included; unmetered only when [unmetered] (P2 §1). [NetworkType] is what WorkManager falls
 * back to where it can't use the request.
 */
fun anyNetwork(unmetered: Boolean = false): Constraints {
    val request = NetworkRequest.Builder()
        .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
        .removeCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
        .removeCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
        .apply { if (unmetered) addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED) }
        .build()
    return Constraints.Builder()
        .setRequiredNetworkRequest(request, if (unmetered) NetworkType.UNMETERED else NetworkType.CONNECTED)
        .build()
}
