package me.tijlvdb.voltis.domain

import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.doubleOrNull

/** A JSON number's value. Null for anything else, a number in a string included. */
fun JsonElement?.numberOrNull(): Double? = (this as? JsonPrimitive)?.takeIf { !it.isString }?.doubleOrNull
