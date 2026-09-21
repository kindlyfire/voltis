type ClickZone = 'prev' | 'next' | 'menu'

const HEIGHT_ZONE = 0.2

export function getClickZone(e: MouseEvent): ClickZone {
    const target = e.currentTarget as HTMLElement
    const rect = target.getBoundingClientRect()

    // Vertical: use visible viewport portion (accounts for navbar/scroll)
    const visibleTop = Math.max(0, rect.top)
    const visibleBottom = Math.min(window.innerHeight, rect.bottom)
    const visibleHeight = visibleBottom - visibleTop
    const relativeY = e.clientY - visibleTop
    const heightPercent = relativeY / visibleHeight

    // Horizontal: use element bounds (accounts for sidebar)
    const centerWidth = Math.min(rect.width / 3, 300)
    const widthZone1 = (rect.width - centerWidth) / 2
    const widthZone2 = widthZone1 + centerWidth
    const relativeX = e.clientX - rect.left

    if (heightPercent < HEIGHT_ZONE) {
        return 'prev'
    }
    if (heightPercent > 1 - HEIGHT_ZONE) {
        return 'next'
    }
    if (relativeX < widthZone1) {
        return 'prev'
    }
    if (relativeX > widthZone2) {
        return 'next'
    }
    return 'menu'
}

export function isTyping(): boolean {
    const active = document.activeElement
    return (
        active instanceof HTMLInputElement ||
        active instanceof HTMLTextAreaElement ||
        active instanceof HTMLSelectElement ||
        (active instanceof HTMLElement && active.isContentEditable)
    )
}
