# Runtime stage - using chromedp's docker image which includes Chrome
FROM chromedp/headless-shell:latest

# Copy binary from goreleaser build context
COPY stelloauth /usr/local/bin/stelloauth

# Expose port
EXPOSE 8080

# Run the application
ENTRYPOINT ["/usr/local/bin/stelloauth"]
