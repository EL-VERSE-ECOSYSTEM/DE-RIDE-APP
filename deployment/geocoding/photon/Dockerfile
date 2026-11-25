FROM gradle:8-jdk21 AS build
WORKDIR /src
RUN git clone --depth 1 --branch v1.3.0 https://github.com/komoot/photon.git .
RUN ./gradlew --no-daemon shadowJar

FROM eclipse-temurin:21-jre
RUN apt-get update && apt-get install -y --no-install-recommends netcat-openbsd \
 && rm -rf /var/lib/apt/lists/*
RUN useradd --system --home-dir /photon --create-home photon
WORKDIR /photon
COPY --from=build --chown=photon:photon /src/target/photon-opensearch-*.jar /photon/photon.jar
COPY --chown=photon:photon entrypoint.sh /usr/local/bin/photon-entrypoint
RUN chmod 0755 /usr/local/bin/photon-entrypoint
USER photon
EXPOSE 2322
ENTRYPOINT ["/usr/local/bin/photon-entrypoint"]
