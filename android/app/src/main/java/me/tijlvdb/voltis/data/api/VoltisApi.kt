package me.tijlvdb.voltis.data.api

import kotlinx.serialization.json.JsonObject
import retrofit2.Response
import retrofit2.http.Body
import retrofit2.http.GET
import retrofit2.http.Header
import retrofit2.http.POST
import retrofit2.http.Url

/**
 * Relative paths go to the current server (see [ServerUrlInterceptor]); `@Url` methods take an
 * absolute URL and are neither rewritten nor given the stored bearer.
 */
interface VoltisApi {
    @GET("api/info")
    suspend fun info(): Info

    /** The Server screen's check of a URL that isn't saved yet; the raw response has the final URL. */
    @GET
    suspend fun probeInfo(@Url url: String): Response<Info>

    @POST("api/auth/token")
    suspend fun token(@Body body: TokenRequest): TokenResponse

    /** Exchanges at the browser flow's server, which may no longer be the current one. */
    @POST
    suspend fun exchangeAt(@Url url: String, @Body body: ExchangeRequest): TokenResponse

    /** The server requires a JSON body, even an empty one. */
    @POST("api/auth/logout")
    suspend fun logout(@Body body: JsonObject = JsonObject(emptyMap()))

    @GET("api/users/me")
    suspend fun me(): Me

    /** Checks a new token before it's stored, so a sign-in saves token and user together. */
    @GET
    suspend fun meAt(@Url url: String, @Header("Authorization") authorization: String): Me
}
