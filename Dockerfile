# Runtime stage - the app no longer launches Chrome (a CloakBrowser sidecar does).
# Distroless static: CA certs included, no shell, runs as nonroot. Pinned by
# digest (Renovate keeps it current).
FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35

# Copy binary from goreleaser build context
ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/stelloauth /usr/local/bin/stelloauth

# Expose port
EXPOSE 8080

# Run the application
ENTRYPOINT ["/usr/local/bin/stelloauth"]
