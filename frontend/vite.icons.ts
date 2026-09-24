import Icons from 'unplugin-icons/vite'

/** Icons are decorative by default; AIcon adds a label when one is meaningful. */
export const iconsPlugin = () =>
    Icons({
        compiler: 'vue3',
        iconCustomizer(_collection, _icon, props) {
            props.width = '1em'
            props.height = '1em'
            props['aria-hidden'] = 'true'
        },
    })
