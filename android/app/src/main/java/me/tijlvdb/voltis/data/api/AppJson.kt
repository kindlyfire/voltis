package me.tijlvdb.voltis.data.api

import kotlinx.serialization.json.Json

/** The one Json configuration, for the API and for stored state. */
val AppJson = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
}
