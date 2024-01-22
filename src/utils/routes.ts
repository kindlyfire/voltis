export const routeBuilder = {
	'/': () => '/',
	'/lists': () => '/lists',
	'/search': () => '/search',
	'/admin/libraries': () => '/admin/libraries',
	'/admin/summary': () => '/admin/summary',
	'/auth/login': () => '/auth/login',
	'/auth/register': () => '/auth/register',
	'/c/[collectionId]': (collectionId: string) => `/c/${collectionId}`,
	'/c/[collectionId]/[name]': (collectionId: string, name: string) =>
		`/c/${collectionId}/${name}`,
	'/read/[itemId]/[page]': (itemId: string, page: string | number) =>
		`/read/${itemId}/${page}`,
	'/user/account': () => '/user/account',
	'/user/preferences': () => '/user/preferences'
}
