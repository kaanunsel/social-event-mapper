# syntax=docker/dockerfile:1
# Production web image built from repository root so Swagger UI and OpenAPI specs are bundled.
FROM node:20-alpine AS build

ARG VITE_GOOGLE_MAPS_WEB_API_KEY
ENV VITE_GOOGLE_MAPS_WEB_API_KEY=$VITE_GOOGLE_MAPS_WEB_API_KEY

WORKDIR /app
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install
COPY frontend/ .
RUN npm run build

FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf
COPY docs/openapi /usr/share/nginx/html/docs/openapi
COPY docs/swagger-ui /usr/share/nginx/html/docs/swagger-ui
EXPOSE 80
