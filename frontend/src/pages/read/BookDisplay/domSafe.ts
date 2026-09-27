/** Prototype-bound DOM access. Book markup is untrusted, and an element's own
 * named properties shadow inherited members — `<img name="querySelectorAll">`
 * shadows it on the document — so traversals must not go through instances. */
const get = Element.prototype.getAttribute
const set = Element.prototype.setAttribute
const remove = Element.prototype.removeAttribute
const has = Element.prototype.hasAttribute
const documentBody = Object.getOwnPropertyDescriptor(Document.prototype, 'body')!.get!
const documentRoot = Object.getOwnPropertyDescriptor(Document.prototype, 'documentElement')!.get!
const createWalker = Document.prototype.createTreeWalker
const ownerDocument = Object.getOwnPropertyDescriptor(Node.prototype, 'ownerDocument')!.get!
const childNodes = Object.getOwnPropertyDescriptor(Node.prototype, 'childNodes')!.get!
const children = Object.getOwnPropertyDescriptor(Element.prototype, 'children')!.get!
const parent = Object.getOwnPropertyDescriptor(Node.prototype, 'parentNode')!.get!
const previous = Object.getOwnPropertyDescriptor(Node.prototype, 'previousSibling')!.get!
const tagName = Object.getOwnPropertyDescriptor(Element.prototype, 'tagName')!.get!

export const getAttr = (el: Element, name: string): string | null => get.call(el, name)
export const setAttr = (el: Element, name: string, value: string) => set.call(el, name, value)
export const removeAttr = (el: Element, name: string) => remove.call(el, name)
export const hasAttr = (el: Element, name: string): boolean => has.call(el, name)
export const tagOf = (el: Element): string => (tagName.call(el) as string).toLowerCase()
export const childNodesOf = (node: Node): Node[] => Array.from(childNodes.call(node) as NodeList)
export const childrenOf = (el: Element): Element[] =>
    Array.from(children.call(el) as HTMLCollection)
export const parentOf = (node: Node): Node | null => parent.call(node) as Node | null
export const previousOf = (node: Node): Node | null => previous.call(node) as Node | null

export const docBody = (doc: Document): HTMLElement | null =>
    documentBody.call(doc) as HTMLElement | null

export const docRoot = (doc: Document): Element => documentRoot.call(doc) as Element

/** Preorder element walk. Selector engines reach for members of the node's own
 * document, which book markup can shadow, so parsed content is walked, never
 * queried. */
export function elementsUnder(root: Node): Element[] {
    const out: Element[] = []
    const walk = (node: Node) => {
        for (const child of childNodesOf(node)) {
            if (child.nodeType !== Node.ELEMENT_NODE) continue
            out.push(child as Element)
            walk(child)
        }
    }
    walk(root)
    return out
}

export function textWalkerFor(root: Node): TreeWalker {
    const doc = (ownerDocument.call(root) as Document | null) ?? (root as Document)
    return createWalker.call(doc, root, NodeFilter.SHOW_TEXT)
}

export function findTarget(root: Node, fragment: string): Element | null {
    if (!fragment) return null
    const elements =
        root.nodeType === Node.ELEMENT_NODE
            ? [root as Element, ...elementsUnder(root)]
            : elementsUnder(root)
    for (const el of elements) {
        if (getAttr(el, 'id') === fragment) return el
    }
    for (const el of elements) {
        if (getAttr(el, 'name') === fragment) return el
    }
    return null
}

/** Chrome keeps a selection made inside a shadow root out of the window's own
 * and exposes it on the root instead. */
export function shadowSelection(root: ShadowRoot): Selection | null {
    return (root as ShadowRoot & { getSelection?(): Selection | null }).getSelection?.() ?? null
}
