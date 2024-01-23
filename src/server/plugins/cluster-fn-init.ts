/**
 * This plugin must import any files that initialize a function using
 * defineClusterFn().
 */
export default defineNitroPlugin(async () => {
	await import('../utils/comic-reader')
	await import('../scanning/scanner')
})
