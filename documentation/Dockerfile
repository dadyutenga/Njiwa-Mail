# Documentation site: build the static export, serve it with unprivileged nginx.
# No secrets are needed or accepted at build or run time. The only build inputs are public:
#   DOCS_BASE_URL  the public origin of the docs (used in llms.txt and prompts)
#   DOCS_COMMIT    the source commit shown in every generated file
FROM node:24-bookworm-slim AS build
WORKDIR /repo
COPY docs/openapi.yaml docs/openapi.yaml
COPY skills skills
COPY clients/typescript clients/typescript
WORKDIR /repo/documentation
COPY documentation/package.json documentation/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY documentation/ ./
ARG DOCS_BASE_URL=https://docs.example.com
ARG DOCS_COMMIT=dev
ENV NEXT_PUBLIC_DOCS_BASE_URL=$DOCS_BASE_URL NEXT_PUBLIC_DOCS_COMMIT=$DOCS_COMMIT NEXT_TELEMETRY_DISABLED=1
RUN node node_modules/fumadocs-mdx/bin.js && node scripts/lint-content.mjs && npm run build && node scripts/check-export.mjs

FROM nginxinc/nginx-unprivileged:1.27-alpine
COPY documentation/nginx.conf /etc/nginx/nginx.conf
COPY --from=build /repo/documentation/out /usr/share/nginx/html
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD wget -q -O /dev/null http://127.0.0.1:8080/docs || exit 1
