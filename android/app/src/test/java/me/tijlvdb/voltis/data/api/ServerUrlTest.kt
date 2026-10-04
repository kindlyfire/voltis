package me.tijlvdb.voltis.data.api

import okhttp3.HttpUrl.Companion.toHttpUrl
import org.junit.Assert.assertEquals
import org.junit.Test

class ServerUrlTest {
    @Test
    fun normalize() {
        val cases = mapOf(
            "  https://voltis.example/  " to "https://voltis.example/",
            "https://voltis.example/sub/path//" to "https://voltis.example/sub/path",
            "HTTP://192.168.1.5:8080/?x=1#y" to "http://192.168.1.5:8080/",
            "voltis.example" to null,
            "ftp://voltis.example" to null,
            "https://user:pw@voltis.example/" to "https://voltis.example/",
            "https://" to null,
        )
        for ((input, want) in cases) {
            assertEquals(input, want, ServerUrl.normalize(input)?.toString())
        }
        // Where a probe ended, back to the server URL.
        assertEquals("https://a.example/v", ServerUrl.fromInfoUrl("https://a.example/v/api/info".toHttpUrl())?.toString())
        assertEquals(null, ServerUrl.fromInfoUrl("https://a.example/login".toHttpUrl()))
    }

    @Test
    fun localHosts() {
        // URL, plain http is flagged insecure, needs the local-network permission.
        val cases = listOf(
            Triple("https://voltis.example", false, false),
            Triple("http://voltis.example", true, false),
            Triple("http://8.8.8.8", true, false),
            Triple("http://localhost:8080", false, false),
            Triple("http://127.0.0.1", false, false),
            Triple("http://[::1]", false, false),
            Triple("http://nas", false, true),
            Triple("http://nas.local", false, true),
            Triple("http://NAS.local.", false, true),
            Triple("http://10.0.2.2", false, true),
            Triple("http://192.168.1.5", false, true),
            Triple("http://192.168", true, false),
            Triple("http://192.168.1.999", true, false),
            Triple("http://100.101.102.103", false, true),
            Triple("http://[fd00::1]", false, true),
            Triple("http://[2001:db8::1]", true, false),
        )
        for ((text, insecure, permission) in cases) {
            val url = text.toHttpUrl()
            assertEquals(text, insecure, ServerUrl.isInsecure(url))
            assertEquals(text, permission, ServerUrl.needsLocalNetwork(url.host))
        }
    }
}
