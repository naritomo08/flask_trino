FROM nginx:1.27-alpine

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY index.html /usr/share/nginx/html/index.html
COPY health.html /usr/share/nginx/html/health.html
COPY health.js /usr/share/nginx/html/health.js
COPY search.js /usr/share/nginx/html/search.js
COPY styles.css /usr/share/nginx/html/styles.css
