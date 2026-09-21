import z from 'zod'
import { useLocalStorage } from '@/utils/localStorage'
import { MONO_STACK } from './prepareDocument'

export const FONT_STACKS = {
    serif: "Georgia, 'Times New Roman', serif",
    sans: "system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif",
    mono: MONO_STACK,
    'fast-serif': "'Fast Serif', Georgia, serif",
    'fast-sans': "'Fast Sans', system-ui, sans-serif",
    dyslexic: "'OpenDyslexic', Verdana, sans-serif",
} as const

export type BookFont = keyof typeof FONT_STACKS

/** `publisher` is the absence of a family rather than one more family: it
 * leaves the book's own font rules standing. */
export type FontChoice = BookFont | 'publisher'

export const FONT_OPTIONS: Array<{ value: FontChoice; title: string }> = [
    { value: 'publisher', title: 'Publisher' },
    { value: 'serif', title: 'Serif' },
    { value: 'sans', title: 'Sans' },
    { value: 'mono', title: 'Mono' },
    { value: 'fast-serif', title: 'Fast Serif' },
    { value: 'fast-sans', title: 'Fast Sans' },
    { value: 'dyslexic', title: 'OpenDyslexic' },
]

/** Per field, so one stale or out-of-range value can't discard the rest. */
export const zBookSettings = z.object({
    /** rem */
    fontSize: z.number().min(0.7).max(2.5).catch(1.1),
    fontFamily: z.enum(FONT_OPTIONS.map(option => option.value)).catch('serif'),
    lineHeight: z.number().min(1.1).max(2.5).catch(1.8),
    /** em, so the measure follows the font size */
    width: z.number().min(20).max(80).catch(45),
})

export type BookSettings = z.infer<typeof zBookSettings>

export function useBookSettings() {
    return useLocalStorage('reader:book-settings', v => {
        const found = zBookSettings.safeParse(v)
        return found.success ? found.data : zBookSettings.parse({})
    }).value
}
