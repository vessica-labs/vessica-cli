FROM node:24.13.0-bookworm-slim AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY tsconfig.json ./
COPY src ./src
RUN npm run build && npm prune --omit=dev

FROM node:24.13.0-bookworm-slim
ENV NODE_ENV=production PORT=8080
WORKDIR /app
RUN groupadd --system runtime && useradd --system --gid runtime --home-dir /app runtime
COPY --from=build --chown=runtime:runtime /app/package.json /app/package-lock.json ./
COPY --from=build --chown=runtime:runtime /app/node_modules ./node_modules
COPY --from=build --chown=runtime:runtime /app/dist ./dist
USER runtime
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=4 CMD node -e "fetch('http://127.0.0.1:'+process.env.PORT+'/healthz').then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))"
CMD ["node", "dist/index.js"]
