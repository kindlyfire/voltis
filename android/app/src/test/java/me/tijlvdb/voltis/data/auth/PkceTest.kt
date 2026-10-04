package me.tijlvdb.voltis.data.auth

import org.junit.Assert.assertEquals
import org.junit.Test

class PkceTest {
    @Test
    fun rfc7636AppendixB() {
        assertEquals(
            "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
            Pkce.challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"),
        )
    }
}
