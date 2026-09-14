import { configDefaults, defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'

export default defineConfig({
  plugins: [
    vue(),
    Components({ resolvers: [ElementPlusResolver()] }),
  ],
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:8080',
      '/v1': 'http://127.0.0.1:8080',
    },
  },
  test: {
    environment: 'jsdom',
    exclude: [...configDefaults.exclude, 'e2e/**'],
    // Element Plus 的自动引入会带上 `element-plus/es/components/*/style/css` 这类副作用样式导入。
    // 默认这些依赖被外部化交给 Node 加载，Node 不认识 .css 会直接报「Unknown file extension」，
    // 于是任何挂载含 el-* 组件的测试都无法启动；内联后由 Vite 处理这些样式导入。
    server: { deps: { inline: [/element-plus/] } },
  },
})
