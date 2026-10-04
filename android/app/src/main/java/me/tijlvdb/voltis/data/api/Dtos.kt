package me.tijlvdb.voltis.data.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Hand-ported from frontend/src/utils/api/{misc,types}.ts.

/** The parts of the web's `Info` this app uses. */
@Serializable
data class Info(
    // Defaults let an older server decode, so the app can say it's too old.
    @SerialName("api_version") val apiVersion: Int = 0,
    @SerialName("server_id") val serverId: String = "",
    @SerialName("first_user_flow") val firstUserFlow: Boolean,
    @SerialName("password_login_enabled") val passwordLoginEnabled: Boolean,
    @SerialName("oidc_enabled") val oidcEnabled: Boolean,
    @SerialName("oidc_button_label") val oidcButtonLabel: String,
)

/** The parts of the web's `Me` this app uses. */
@Serializable
data class Me(
    val id: String,
    val username: String,
    /** One of [SessionMethod]; a String so a new method doesn't break decoding. */
    @SerialName("session_method") val sessionMethod: String,
)

object SessionMethod {
    const val PASSWORD = "password"
    const val OIDC = "oidc"
    const val PROXY = "proxy"
}

@Serializable
data class ErrorResponse(val error: String)

@Serializable
data class TokenRequest(
    val username: String,
    val password: String,
    @SerialName("client_name") val clientName: String,
)

@Serializable
data class ExchangeRequest(
    val code: String,
    @SerialName("code_verifier") val codeVerifier: String,
)

@Serializable
data class TokenResponse(val token: String)
