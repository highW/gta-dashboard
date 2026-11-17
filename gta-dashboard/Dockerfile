# Image for the frontend: builds the React app, then serves it with nginx.
# nginx also forwards /api/ to the load balancer, so the browser talks to
# one address and needs no CORS.

FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
# The app calls the API on its own address, under /api.
ENV VITE_API_URL=/api
RUN npm run build

FROM nginx:1.29-alpine
COPY docker/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
