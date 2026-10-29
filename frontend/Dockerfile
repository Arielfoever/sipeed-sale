# syntax=docker/dockerfile:1
# 前端镜像:Vite 构建 dist → nginx 托管(SPA fallback + /api 反代到后端容器)
# 注意:src/schema.d.ts 已由 `npm run gen:api` 生成并入库
# (CI/本地都会校验其与 openapi.yaml 同步),镜像构建时不重新生成。
FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine AS runtime
COPY nginx-dev.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
