FROM node:22-alpine AS dependencies

WORKDIR /app
COPY clients/web/package.json clients/web/package-lock.json ./
RUN npm ci

FROM node:22-alpine AS web-build

WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1
COPY --from=dependencies /app/node_modules ./node_modules
COPY clients/web ./
RUN npm run build

FROM node:22-alpine AS runner

WORKDIR /app
ENV HOSTNAME=0.0.0.0
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
RUN addgroup --system --gid 1001 nodejs && adduser --system --uid 1001 nextjs
COPY --from=web-build --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=web-build --chown=nextjs:nodejs /app/.next/static ./.next/static
COPY --from=web-build --chown=nextjs:nodejs /app/public ./public
USER nextjs
EXPOSE 3000
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 CMD wget -q -O - http://127.0.0.1:3000/ || exit 1
CMD ["node", "server.js"]
