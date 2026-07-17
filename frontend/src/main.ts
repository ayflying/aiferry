import { createApp } from 'vue'
import { createPinia } from 'pinia'
import '@fontsource-variable/ibm-plex-sans'
import '@fontsource/jetbrains-mono/500.css'
import 'element-plus/es/components/message-box/style/css'
import './styles.css'
import App from './App.vue'
import router from './router'

createApp(App).use(createPinia()).use(router).mount('#app')
