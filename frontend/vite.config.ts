import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import { mockApiPlugin } from "./src/mocks/mockApi";

// 开发期的 /api 代理目标:默认指向本地 Go 后端的服务端口。
// 可在 frontend/.env.local 中用 VITE_API_PROXY_TARGET 覆盖。
//
// Mock 模式:后端未就绪时,`npm run dev:mock`(--mode mock,或设置
// VITE_API_MOCK=1)会在 dev server 内直接模拟 /api,无需启动 Go 服务。
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const apiProxyTarget = env.VITE_API_PROXY_TARGET ?? "http://localhost:8080";
  const mockEnabled = mode === "mock" || env.VITE_API_MOCK === "1";

  return {
    plugins: [react(), ...(mockEnabled ? [mockApiPlugin()] : [])],
    server: {
      port: 5173,
      proxy: mockEnabled
        ? undefined
        : {
            // mock 未开启时,/api 由真实后端接管:请求被代理并在后端
            // 未启动时表现为 404 / ECONNREFUSED,属于预期行为。
            "/api": {
              target: apiProxyTarget,
              changeOrigin: true,
            },
          },
    },
  };
});
