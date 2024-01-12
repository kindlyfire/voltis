/**
 * System that lets the code vote/decide on which properties of something to
 * send to the client, based on a set of rules
 */

type MaybeArray<T> = T | Array<T>

interface VoterCallbackData<
	TObject extends Record<string, any>,
	TContext extends {}
> {
	object: TObject
	context: TContext
	allow(k: MaybeArray<keyof TObject>): void
	allowAll(): void
	deny(k: MaybeArray<keyof TObject>): void
}

interface VoterCallback<
	TObject extends Record<string, any>,
	TContext extends {}
> {
	(ctx: VoterCallbackData<TObject, TContext>): void
}

type VoterOptionsDefaults<TObject extends Record<string, any>> = {
	[k in keyof TObject]?: boolean
}

type VoterOptionsKeyVotersCallbackData<
	TObject extends Record<string, any>,
	TContext extends {}
> = {}

type VoterOptionsKeyVoters<
	TObject extends Record<string, any>,
	TContext extends {}
> = {
	[k in keyof TObject]?: (
		data: VoterOptionsKeyVotersCallbackData<TObject, TContext>
	) => Partial<TObject[k]> | false
}

interface VoterOptions<
	TObject extends Record<string, any>,
	TContext extends {}
> {
	defaults: VoterOptionsDefaults<TObject>
	vote: MaybeArray<VoterCallback<TObject, TContext>>
	// TODO: Implement key voters
	// keyVoters?: VoterOptionsKeyVoters<TObject, TContext>
}
interface Voter<TObject extends Record<string, any>, TContext extends {}> {
	run: (object: TObject, context: TContext) => Partial<TObject>
}
export function createVoter<
	TObject extends Record<string, any>,
	TContext extends {}
>(options: VoterOptions<TObject, TContext>): Voter<TObject, TContext> {
	return {
		run(object, context) {
			const keyStates = Object.fromEntries(
				Object.keys(options.defaults).map(key => [key, 0])
			) as Record<keyof TObject, number>

			const setKeysTo = (keys: MaybeArray<keyof TObject>, state: -1 | 1) => {
				const keysArr = Array.isArray(keys) ? keys : [keys]
				for (const key of keysArr.filter(k => k in keyStates)) {
					keyStates[key] = keyStates[key] != -1 ? state : -1
				}
			}
			const cb: VoterCallbackData<TObject, TContext> = {
				object,
				context,
				allowAll() {
					for (const key in keyStates) {
						keyStates[key] = 1
					}
				},
				allow(k) {
					setKeysTo(k, 1)
				},
				deny(k) {
					setKeysTo(k, -1)
				}
			}

			const voteFns = Array.isArray(options.vote)
				? options.vote
				: [options.vote]
			for (const voteFn of voteFns) {
				voteFn(cb)
			}

			// Apply defaults where the state is still 0
			for (const key in keyStates) {
				if (keyStates[key] == 0) {
					keyStates[key] = options.defaults[key] ? 1 : -1
				}
			}

			// Apply key voters
			const obj = {} as Partial<TObject>
			for (const key in keyStates) {
				if (keyStates[key] == 1) {
					obj[key] = object[key]
				}
			}

			return obj
		}
	}
}
