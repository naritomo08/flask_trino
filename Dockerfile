FROM composer:2 AS vendor

WORKDIR /app
COPY composer.json composer.lock .
RUN composer install --no-dev --no-interaction --prefer-dist --optimize-autoloader

FROM php:8.3-apache

RUN a2enmod rewrite \
    && sed -ri -e 's!/var/www/html!/var/www/html/public!g' /etc/apache2/sites-available/*.conf /etc/apache2/apache2.conf \
    && sed -ri -e 's/AllowOverride None/AllowOverride All/g' /etc/apache2/apache2.conf \
    && sed -ri -e 's/Listen 80/Listen 5000/g' /etc/apache2/ports.conf \
    && sed -ri -e 's/<VirtualHost \*:80>/<VirtualHost *:5000>/g' /etc/apache2/sites-available/*.conf

WORKDIR /var/www/html

COPY . /var/www/html/
COPY --from=vendor /app/vendor /var/www/html/vendor

EXPOSE 5000
