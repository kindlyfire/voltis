package me.tijlvdb.voltis.data.api

import java.net.InetAddress
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

/** Host of relative Retrofit requests; [ServerUrlInterceptor] points them at the current server. */
const val PLACEHOLDER_HOST = "voltis.invalid"

object ServerUrl {
    /** Trims, requires an http(s) scheme, and drops userinfo, query, fragment and a trailing `/`. */
    fun normalize(input: String): HttpUrl? {
        val text = input.trim()
        if (!text.startsWith("http://", true) && !text.startsWith("https://", true)) return null
        val url = text.toHttpUrlOrNull() ?: return null
        return base(url, url.encodedPath)
    }

    /** The server URL behind the `api/info` URL a probe ended at, which may follow redirects. */
    fun fromInfoUrl(url: HttpUrl): HttpUrl? =
        url.encodedPath.takeIf { it.endsWith("/$INFO_PATH") }?.let { base(url, it.removeSuffix(INFO_PATH)) }

    private fun base(url: HttpUrl, path: String) = url.newBuilder()
        .username("")
        .password("")
        .query(null)
        .fragment(null)
        .encodedPath(path.trimEnd('/').ifEmpty { "/" })
        .build()

    /** True for plain http to a host that doesn't look local. */
    fun isInsecure(url: HttpUrl): Boolean = !url.isHttps && !isLocalHost(url.host)

    /** A private, loopback or link-local address, or a name that isn't public (`nas`, `nas.local`). */
    fun isLocalHost(host: String): Boolean {
        val h = host.lowercase().removeSuffix(".")
        if ('.' !in h && ':' !in h) return true // localhost, bare LAN names
        if (LOCAL_SUFFIXES.any { h.endsWith(it) }) return true
        val addr = ipLiteral(h) ?: return false
        val b = addr.address
        return addr.isLoopbackAddress || addr.isSiteLocalAddress || addr.isLinkLocalAddress ||
            (b.size == 16 && b[0].toInt() and 0xfe == 0xfc) || // IPv6 ULA
            (b.size == 4 && b[0].toInt() == 100 && b[1].toInt() and 0xc0 == 64) // CGNAT, e.g. Tailscale
    }

    /** Local but not this device: what Android 17 may gate behind the local-network permission. */
    fun needsLocalNetwork(host: String): Boolean {
        val h = host.lowercase().removeSuffix(".")
        return isLocalHost(h) && h != "localhost" && ipLiteral(h)?.isLoopbackAddress != true
    }

    /** This device: the server needs no network to be reached (the emulator's `adb reverse`, a server on the phone). */
    fun isLoopback(host: String): Boolean {
        val h = host.lowercase().removeSuffix(".")
        return h == "localhost" || ipLiteral(h)?.isLoopbackAddress == true
    }

    private fun ipLiteral(h: String): InetAddress? {
        // Never resolve a name: this runs on the main thread as the user types, and Android looks up
        // partial addresses such as "192.168". OkHttp only keeps a ':' in a valid IPv6 literal.
        if (':' in h) return InetAddress.getByName(h)
        val octets = h.split('.').map { p -> p.takeIf { it.length in 1..3 && it.all(Char::isDigit) }?.toInt() }
        if (octets.size != 4 || octets.any { it == null || it > 255 }) return null
        return InetAddress.getByAddress(ByteArray(4) { octets[it]!!.toByte() })
    }

    const val INFO_PATH = "api/info"
    private val LOCAL_SUFFIXES = listOf(".local", ".lan", ".home.arpa", ".internal")
}

fun HttpUrl.sameOrigin(other: HttpUrl) = scheme == other.scheme && host == other.host && port == other.port

/** [path] resolved under this server URL, keeping any path prefix. */
fun HttpUrl.api(path: String): String = newBuilder().addPathSegments(path).build().toString()
