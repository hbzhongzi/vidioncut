import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import './style.css'

// v1.6 UI 改版：组件全部自绘（与深色主题严格对齐），移除 Element Plus 依赖
createApp(App).use(createPinia()).mount('#app')
