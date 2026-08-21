# Runtime stage - the app no longer launches Chrome (a CloakBrowser sidecar does).
# Distroless static: CA certs included, no shell, runs as nonroot. Pinned by
# digest (Renovate keeps it current).
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

# Copy binary from goreleaser build context
ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/stelloauth /usr/local/bin/stelloauth

# Expose port
EXPOSE 8080

# Run the application
ENTRYPOINT ["/usr/local/bin/stelloauth"]
