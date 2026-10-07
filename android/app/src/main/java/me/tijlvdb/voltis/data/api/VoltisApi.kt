package me.tijlvdb.voltis.data.api

import me.tijlvdb.voltis.data.auth.SessionStore
import kotlinx.serialization.json.JsonObject
import me.tijlvdb.voltis.domain.reading.Envelope
import me.tijlvdb.voltis.domain.reading.ReadingResult
import me.tijlvdb.voltis.domain.reading.SeriesReceipt
import retrofit2.Call
import retrofit2.Response
import retrofit2.http.Body
import retrofit2.http.DELETE
import retrofit2.http.GET
import retrofit2.http.HTTP
import retrofit2.http.Header
import retrofit2.http.Headers
import retrofit2.http.PATCH
import retrofit2.http.POST
import retrofit2.http.Path
import retrofit2.http.Query
import retrofit2.http.QueryMap
import retrofit2.http.Tag
import retrofit2.http.Url

/**
 * Relative paths go to the current server (see [ServerUrlInterceptor]); `@Url` methods take an
 * absolute URL and are neither rewritten nor given the stored bearer.
 */
interface VoltisApi {
    @GET("api/info")
    suspend fun info(): Info

    /**
     * `Connectivity`'s probe of the current server, with short timeouts (seconds). A blocking call: an
     * interceptor may wait for it while holding one of the dispatcher's slots for the host.
     */
    @GET("api/info")
    fun probe(
        @Tag tag: ConnectivityProbe,
        @Header(TimeoutInterceptor.CONNECT_HEADER) connectTimeout: Int,
        @Header(TimeoutInterceptor.HEADER) readTimeout: Int,
    ): Call<Info>

    /** The Server screen's check of a URL that isn't saved yet; the raw response has the final URL. */
    @GET
    suspend fun probeInfo(@Url url: String): Response<Info>

    @POST("api/auth/token")
    suspend fun token(@Body body: TokenRequest): TokenResponse

    /** Exchanges at the browser flow's server, which may no longer be the current one. */
    @POST
    suspend fun exchangeAt(@Url url: String, @Body body: ExchangeRequest): TokenResponse

    /** The server requires a JSON body, even an empty one. Short timeouts (seconds): signing out never waits on a server that's gone. */
    @POST("api/auth/logout")
    suspend fun logout(
        @Header(TimeoutInterceptor.CONNECT_HEADER) connectTimeout: Int = 3,
        @Header(TimeoutInterceptor.HEADER) readTimeout: Int = 3,
        @Body body: JsonObject = JsonObject(emptyMap()),
        /** The session to end: the request goes to its server with its bearer, whatever is current when it runs. */
        @Tag session: SessionStore.Active? = null,
    )

    @GET("api/users/me")
    suspend fun me(): Me

    /** A merge patch: a null deletes a member. A JsonObject, so the nulls are sent rather than dropped. */
    @PATCH("api/users/me/preferences")
    suspend fun patchPreferences(@Body patch: JsonObject): User

    @GET("api/users/me/sessions")
    suspend fun sessions(): List<Session>

    /** Answers 404 for the session that asks. The server requires a JSON body. */
    @HTTP(method = "DELETE", path = "api/users/me/sessions/{id}", hasBody = true)
    suspend fun revokeSession(@Path("id") id: String, @Body body: JsonObject = JsonObject(emptyMap()))

    @GET("api/libraries")
    suspend fun libraries(): List<Library>

    @GET("api/content")
    suspend fun content(@QueryMap params: Map<String, String>, @Tag account: ForAccount? = null): ContentPage

    @GET("api/content/continue-reading")
    suspend fun continueReading(
        @Query("limit") limit: Int,
        @Header(TimeoutInterceptor.CONNECT_HEADER) connectTimeout: Int? = null,
        @Header(TimeoutInterceptor.HEADER) readTimeout: Int? = null,
        @Tag account: ForAccount? = null,
    ): List<ContinueEntry>

    @GET("api/content/{id}")
    suspend fun contentById(@Path("id") id: String, @Tag account: ForAccount? = null): Content

    /** The server measures an unsized archive on this request, which can take minutes. */
    @Headers("${TimeoutInterceptor.HEADER}: 330")
    @GET("api/content/{id}?page_sizes=1")
    suspend fun comicWithPageSizes(@Path("id") id: String, @Tag account: ForAccount? = null): Content

    /** Answers 400 without a `limit`. */
    @GET("api/content/ids")
    suspend fun contentIds(@QueryMap params: Map<String, String>): ContentIds

    @GET("api/content/{id}/continue")
    suspend fun continueTarget(@Path("id") id: String, @Tag account: ForAccount? = null): ContinueTarget

    /** Any of `starred` and `rating`. A JsonObject, so a null rating is sent rather than dropped. */
    @POST("api/content/{id}/user-data")
    suspend fun updateUserData(@Path("id") id: String, @Body body: JsonObject, @Tag account: ForAccount? = null): UserData

    /** Null timeouts (seconds) keep the client's. */
    @GET("api/content/{id}/reading")
    suspend fun reading(
        @Path("id") id: String,
        @Header(TimeoutInterceptor.CONNECT_HEADER) connectTimeout: Int? = null,
        @Header(TimeoutInterceptor.HEADER) readTimeout: Int? = null,
        @Tag account: ForAccount? = null,
    ): Envelope

    /** A JsonObject, so a null status or base revision is sent rather than dropped. */
    @POST("api/content/{id}/reading")
    suspend fun postReading(@Path("id") id: String, @Body body: JsonObject, @Tag account: ForAccount? = null): ReadingResult

    /** A JsonObject: a reader's request carries `writer_id` and `seq`. */
    @POST("api/content/{id}/series-reading")
    suspend fun seriesReading(@Path("id") id: String, @Body body: JsonObject, @Tag account: ForAccount? = null): SeriesReceipt

    /** [kind] is one of `FacetKind`. */
    @GET("api/facets/{kind}")
    suspend fun facets(@Path("kind") kind: String, @QueryMap params: Map<String, String>): FacetPage

    /** Any spelling of [key] finds its value: the server folds it. */
    @GET("api/facets/{kind}/{key}")
    suspend fun facet(@Path("kind") kind: String, @Path("key") key: String, @Query("library_id") libraryId: String?): FacetEntry

    /** The user's own lists only, as the web's Lists page. */
    @GET("api/custom-lists?user=me")
    suspend fun customLists(@Tag account: ForAccount? = null): List<CustomListSummary>

    @GET("api/custom-lists/{id}")
    suspend fun customList(@Path("id") id: String, @Tag account: ForAccount? = null): CustomListDetail

    /** `{name, description, visibility}`; answers the new list. Never sent twice: a repeat would make a second list. */
    @POST("api/custom-lists")
    suspend fun createCustomList(@Body body: JsonObject, @Tag account: ForAccount? = null, @Tag once: SendOnce = SendOnce): CustomListSummary

    /** All three fields, a null description included. */
    @POST("api/custom-lists/{id}")
    suspend fun updateCustomList(@Path("id") id: String, @Body body: JsonObject, @Tag account: ForAccount? = null)

    @DELETE("api/custom-lists/{id}")
    suspend fun deleteCustomList(@Path("id") id: String, @Tag account: ForAccount? = null)

    /** `{list_ids, ids}`: appends each content to each list, skipping entries that exist and unknown IDs. */
    @POST("api/custom-lists/entries")
    suspend fun addListEntries(@Body body: JsonObject, @Tag account: ForAccount? = null): CountResponse

    /** `{ctc_ids}`: entry IDs in their new order. Never sent twice, so a refused connection means it wasn't sent at all. */
    @POST("api/custom-lists/{id}/entries/reorder")
    suspend fun reorderListEntries(@Path("id") id: String, @Body body: JsonObject, @Tag account: ForAccount? = null, @Tag once: SendOnce = SendOnce)

    /** `{notes}`; a null clears them. */
    @POST("api/custom-lists/{id}/entries/{entry}")
    suspend fun updateListEntry(@Path("id") id: String, @Path("entry") entry: String, @Body body: JsonObject, @Tag account: ForAccount? = null)

    @DELETE("api/custom-lists/{id}/entries/{entry}")
    suspend fun deleteListEntry(@Path("id") id: String, @Path("entry") entry: String, @Tag account: ForAccount? = null)

    /** Checks a new token before it's stored, so a sign-in saves token and user together. */
    @GET
    suspend fun meAt(@Url url: String, @Header("Authorization") authorization: String): Me
}
