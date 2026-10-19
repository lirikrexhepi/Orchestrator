import React from 'react'
import {createRoot} from 'react-dom/client'
import '@fontsource-variable/geist/wght.css'
import '@fontsource-variable/geist-mono/wght.css'
import './index.css'
import { initFonts } from './fonts'

import App from './App'

void initFonts()

const preloadPanels = () => {
    void Promise.all([
        import('./components/sidebar/SettingsPanel'),
        import('./components/sidebar/SettingsWindow'),
    ])
}

requestAnimationFrame(() => {
    requestAnimationFrame(() => {
        if ('requestIdleCallback' in window) {
            window.requestIdleCallback(preloadPanels, { timeout: 2000 })
        } else {
            globalThis.setTimeout(preloadPanels, 0)
        }
    })
})

const container = document.getElementById('root')

const root = createRoot(container!)

root.render(
    <React.StrictMode>
        <App/>
    </React.StrictMode>
)
