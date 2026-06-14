FROM maven:3.9-eclipse-temurin-21 AS build

WORKDIR /src
COPY pom.xml ./
COPY src ./src
RUN mvn -q test package

FROM eclipse-temurin:21-jre

WORKDIR /app
COPY --from=build /src/target/flask-trino-1.0.0.jar /app/flask-trino.jar

ENV PORT=5000

EXPOSE 5000

ENTRYPOINT ["java", "-jar", "/app/flask-trino.jar"]
