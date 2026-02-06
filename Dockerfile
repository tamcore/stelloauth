# Runtime stage - using chromedp's docker image which includes Chrome
FROM chromedp/headless-shell:latest

# Install CA certificates for HTTPS requests
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*

# Copy binary from goreleaser build context
ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/stelloauth /usr/local/bin/stelloauth

# Expose port
EXPOSE 8080

# Run the application
ENTRYPOINT ["/usr/local/bin/stelloauth"]
