package me.tijlvdb.voltis.data.auth

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import java.io.File
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob

/** Reversible stand-in for the Keystore, which JVM tests don't have. */
object FakeCipher : TokenCipher {
    override fun encrypt(plain: String) = "enc:$plain"

    override fun decrypt(encoded: String) = encoded.removePrefix("enc:")
}

/** A store on [file]; one DataStore per file per process, so pass [scope] to reopen it. */
fun testStore(file: File, scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)) = SessionStore(
    PreferenceDataStoreFactory.create(scope = scope) { file },
    FakeCipher,
    scope,
)
