package me.tijlvdb.voltis.data.auth

import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Base64

/** RFC 7636 S256. */
object Pkce {
    /** 32 random bytes, which encode to 43 characters. */
    fun newVerifier(): String = base64Url(ByteArray(32).also(SecureRandom()::nextBytes))

    fun challenge(verifier: String): String =
        base64Url(MessageDigest.getInstance("SHA-256").digest(verifier.toByteArray(Charsets.US_ASCII)))

    private fun base64Url(bytes: ByteArray) = Base64.getUrlEncoder().withoutPadding().encodeToString(bytes)
}
