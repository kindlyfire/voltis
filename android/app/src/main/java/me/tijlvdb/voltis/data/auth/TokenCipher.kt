package me.tijlvdb.voltis.data.auth

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import java.security.GeneralSecurityException
import java.security.KeyStore
import java.security.ProviderException
import java.util.Base64
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

interface TokenCipher {
    fun encrypt(plain: String): String

    /** Null when the value can't be decrypted any more; the caller then drops it. */
    fun decrypt(encoded: String): String?
}

/** AES-256-GCM with a non-exportable Keystore key; stores `base64(iv ‖ ciphertext)`. */
class KeystoreTokenCipher : TokenCipher {
    private val keyStore by lazy { KeyStore.getInstance(KEYSTORE).apply { load(null) } }

    /** A key that stopped working is replaced once; everything stored under the old one is lost. */
    override fun encrypt(plain: String): String = try {
        seal(plain)
    } catch (_: GeneralSecurityException) {
        dropKey()
        seal(plain)
    } catch (_: ProviderException) {
        dropKey()
        seal(plain)
    }

    private fun seal(plain: String): String {
        val cipher = Cipher.getInstance(TRANSFORMATION).apply { init(Cipher.ENCRYPT_MODE, key()) }
        return Base64.getEncoder().encodeToString(cipher.iv + cipher.doFinal(plain.toByteArray()))
    }

    override fun decrypt(encoded: String): String? {
        val bytes = try {
            Base64.getDecoder().decode(encoded)
        } catch (_: IllegalArgumentException) {
            return null
        }
        if (bytes.size <= IV_BYTES) return null
        return try {
            val cipher = Cipher.getInstance(TRANSFORMATION).apply {
                init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(TAG_BITS, bytes, 0, IV_BYTES))
            }
            String(cipher.doFinal(bytes, IV_BYTES, bytes.size - IV_BYTES))
        } catch (_: GeneralSecurityException) {
            dropKey()
        } catch (_: ProviderException) {
            dropKey()
        }
    }

    /** A bad tag, an invalidated key or a broken Keystore: nothing stored can be read again. */
    private fun dropKey(): String? {
        runCatching { keyStore.deleteEntry(ALIAS) }
        return null
    }

    @Synchronized
    private fun key(): SecretKey = keyStore.getKey(ALIAS, null) as SecretKey?
        ?: KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE).run {
            init(
                KeyGenParameterSpec.Builder(
                    ALIAS,
                    KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
                )
                    .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                    .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                    .setKeySize(256)
                    .setRandomizedEncryptionRequired(true)
                    .build(),
            )
            generateKey()
        }

    private companion object {
        const val KEYSTORE = "AndroidKeyStore"
        const val ALIAS = "voltis_token"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val IV_BYTES = 12
        const val TAG_BITS = 128
    }
}
