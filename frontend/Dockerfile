FROM alpine:3.21 AS frontend-builder

WORKDIR /build

COPY build-assets.sh index.html search.js styles.css ./
RUN sh build-assets.sh . dist

FROM nginx:1.27-alpine

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY proxy-common.conf /etc/nginx/includes/proxy-common.conf
COPY --from=frontend-builder /build/dist/ /usr/share/nginx/html/
