import cluster from 'cluster'
import { dbUtils } from '../database/utils'

interface DefineClusterFn<T extends (...args: any[]) => Promise<any>> {
	name: string
	timeout?: number
	fn: T
}

/**
 * Defines a function that can be called from any cluster worker but is only
 * executed on the primary process.
 */
export function defineClusterFn<T extends (...args: any[]) => Promise<any>>(
	options: DefineClusterFn<T>
) {
	if (cluster.isPrimary) {
		cluster.on('message', (worker, msg) => {
			if (isClusterFnRequest(msg) && msg.name === options.name) {
				options
					.fn(...msg.args)
					.then(result => {
						worker.send(<ClusterFnResponse>{
							type: 'cluster-fn-res',
							name: options.name,
							id: msg.id,
							result
						})
					})
					.catch(error => {
						worker.send(<ClusterFnResponse>{
							type: 'cluster-fn-res',
							name: options.name,
							id: msg.id,
							error: error.message
						})
					})
			}
		})
	}

	const f = async (...args: any) => {
		if (cluster.isPrimary) {
			return await options.fn(...args)
		} else {
			const id = dbUtils.createId()
			process.send!(<ClusterFnRequest>{
				type: 'cluster-fn-req',
				name: options.name,
				id,
				args
			})
			const res = await waitForResponse(options.name, id, options.timeout)
			if (res.error) throw new Error(res.error)
			return res.result
		}
	}
	return f as T
}

interface ClusterFnRequest {
	type: 'cluster-fn-req'
	name: string
	id: string
	args: any[]
}

interface ClusterFnResponse {
	type: 'cluster-fn-res'
	name: string
	id: string
	error?: any
	result?: any
}

function isClusterFnRequest(msg: any): msg is ClusterFnRequest {
	return msg.type === 'cluster-fn-req'
}

function isClusterFnResponse(msg: any): msg is ClusterFnResponse {
	return msg.type === 'cluster-fn-res'
}

function waitForResponse(name: string, id: string, waitForMs = 1000 * 30) {
	return new Promise<ClusterFnResponse>((resolve, reject) => {
		const listener = (msg: any) => {
			if (isClusterFnResponse(msg) && msg.name === name && msg.id === id) {
				process.off('message', listener)
				clearTimeout(timeout)
				resolve(msg)
			}
		}
		const timeout = setTimeout(() => {
			process.off('message', listener)
			reject(new Error(`Timeout waiting for cluster response: ${name} ${id}`))
		}, waitForMs)
		process.on('message', listener)
	})
}
