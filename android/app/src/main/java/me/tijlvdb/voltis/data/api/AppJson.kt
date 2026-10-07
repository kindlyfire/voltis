package me.tijlvdb.voltis.data.api

import kotlinx.serialization.json.Json

/** The one Json configuration, for the API and for stored state. */
val AppJson = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
    // A null for a property with a default, or an unknown enum value, takes the default.
    coerceInputValues = true
}
