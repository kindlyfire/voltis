export default defineNitroPlugin(async () => {
	await import('../utils/comic-reader/cluster-primary')
})
