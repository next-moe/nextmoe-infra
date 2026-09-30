#
# The telemetry symbolication worker: the Go worker plus the three external
# decoders it runs — telemetry-decode (package:native_stack_traces, for Dart
# AOT stacks and libapp.so frames), R8 retrace (Java stacks) and
# llvm-symbolizer (libflutter.so frames).
#
#   docker build -f docker/telemetry-worker.Dockerfile -t infra-telemetry-worker .
#
# Build context MUST be the repo root.
ARG GO_VERSION=1.27
ARG DART_VERSION=3.13.3

FROM golang:${GO_VERSION}-trixie AS build
WORKDIR /src
COPY apps/api/go.mod apps/api/go.sum ./
RUN go mod download
COPY apps/api/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o /out/app ./cmd/telemetry-worker

FROM dart:${DART_VERSION} AS decode
WORKDIR /src
COPY docker/telemetry-worker/decode/ ./
RUN mkdir -p /out \
 && dart pub get --enforce-lockfile \
 && dart compile exe bin/telemetry_decode.dart -o /out/telemetry-decode

FROM debian:trixie-slim AS r8
ARG R8_VERSION=9.4.27
ARG R8_SHA256=6646aacebba0e8d13b2309b3b9191d08a2dbd187b648fdc4f78a684bcddade18
RUN apt-get update \
 && apt-get install -y --no-install-recommends curl ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && curl -fsSL -o /r8.jar "https://dl.google.com/android/maven2/com/android/tools/r8/${R8_VERSION}/r8-${R8_VERSION}.jar" \
 && echo "${R8_SHA256}  /r8.jar" | sha256sum -c -

FROM debian:trixie-slim AS runtime
RUN apt-get update \
 && apt-get install -y --no-install-recommends openjdk-21-jre-headless llvm ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 10001 --home-dir /var/cache/telemetry --create-home telemetry
COPY --from=r8 /r8.jar /opt/r8/r8.jar
COPY --from=decode /out/telemetry-decode /usr/local/bin/telemetry-decode
ENV KUN_TELEMETRY_DECODE_BIN=/usr/local/bin/telemetry-decode \
    KUN_TELEMETRY_R8_JAR=/opt/r8/r8.jar \
    KUN_TELEMETRY_JAVA_BIN=/usr/bin/java \
    KUN_TELEMETRY_LLVM_SYMBOLIZER=/usr/bin/llvm-symbolizer \
    KUN_TELEMETRY_SYMBOL_CACHE_DIR=/var/cache/telemetry/symbols

FROM runtime
COPY --from=build /out/app /app
USER telemetry
ENTRYPOINT ["/app"]
