import { API_URL } from '@/utils/fetch'

// Get the directory of the current chapter for resolving relative paths
function getChapterDir(chapterHref: string) {
    const parts = chapterHref.split('/')
    parts.pop()
    return parts.join('/')
}

function resolvePath(chapterHref: string, relativePath: string): string {
    const chapterDir = getChapterDir(chapterHref)

    // Handle absolute paths
    if (relativePath.startsWith('/')) {
        return relativePath.slice(1)
    }

    // Handle relative paths
    const baseParts = chapterDir ? chapterDir.split('/') : []
    const relativeParts = relativePath.split('/')

    for (const part of relativeParts) {
        if (part === '..') {
            baseParts.pop()
        } else if (part !== '.') {
            baseParts.push(part)
        }
    }

    return baseParts.join('/')
}

function rewriteResourceUrl(contentId: string, chapterHref: string, url: string): string {
    if (!url || url.startsWith('data:') || url.startsWith('http')) return url
    const resolvedPath = resolvePath(chapterHref, url)
    return `${API_URL}/files/book-resource/${contentId}?path=${encodeURIComponent(resolvedPath)}`
}

export async function renderChapter(options: {
    chapterContainer: HTMLElement
    chapterHtml: string
    contentId: string
    chapterHref: string
}) {
    const DOMPurify = (await import('dompurify')).default
    DOMPurify.addHook('afterSanitizeAttributes', node => {
        // Rewrite image sources
        if (node.tagName === 'IMG' && node.hasAttribute('src')) {
            node.setAttribute(
                'src',
                rewriteResourceUrl(
                    options.contentId,
                    options.chapterHref,
                    node.getAttribute('src')!
                )
            )
        }
        // Rewrite stylesheet links
        if (
            node.tagName === 'LINK' &&
            node.getAttribute('rel') === 'stylesheet' &&
            node.hasAttribute('href')
        ) {
            node.setAttribute(
                'href',
                rewriteResourceUrl(
                    options.contentId,
                    options.chapterHref,
                    node.getAttribute('href')!
                )
            )
        }
    })
    const sanitized = DOMPurify.sanitize(options.chapterHtml, {
        ADD_TAGS: ['link'],
        ADD_ATTR: ['target', 'rel', 'href'],
        ALLOW_DATA_ATTR: false,
        WHOLE_DOCUMENT: true,
    })
    DOMPurify.removeAllHooks()

    let shadow = options.chapterContainer.shadowRoot
    if (!shadow) {
        shadow = options.chapterContainer.attachShadow({ mode: 'open' })
    }

    shadow.innerHTML = `
		<style>
			:host {
				display: block;
				color-scheme: dark light;
			}
			article {
				font-family: Georgia, 'Times New Roman', serif;
				line-height: 1.8;
				max-width: 45em;
				margin: 0 auto;
				padding: 2rem;
				font-size: 1.1rem;
			}
			img { max-width: 100%; height: auto; }
			a { color: inherit; }
		</style>
		<article>${sanitized}</article>
	`

    window.scrollTo({
        top: 0,
        behavior: 'smooth',
    })
}
